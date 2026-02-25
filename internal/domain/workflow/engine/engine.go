package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/graph"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/port"
	"flowweave/internal/domain/workflow/runtime"
	applog "flowweave/internal/platform/log"
)

// Config 引擎配置
type Config struct {
	MaxWorkers   int           // 最大并行工作线程数
	NodeTimeout  time.Duration // 单个节点执行超时
	MaxNodeSteps int           // 最大节点执行步数
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		MaxWorkers:   4,
		NodeTimeout:  5 * time.Minute,
		MaxNodeSteps: 100,
	}
}

// GraphEngine 队列驱动的图执行引擎
type GraphEngine struct {
	graph        *graph.Graph
	runtimeState *runtime.GraphRuntimeState
	config       *Config
	logger       *slog.Logger

	// 就绪队列（待执行的节点）
	readyQueue chan string
	// 事件队列（节点执行产出的事件）
	eventQueue chan event.NodeEvent
	// 完成信号
	completedNodes sync.Map // node_id -> bool
	// 活跃节点计数（原子操作，避免竞态）
	pendingCount atomic.Int32
	// 用于安全关闭 readyQueue
	closeOnce sync.Once

	// 命令通道
	commandCh chan types.Command
	// 暂停控制
	pauseMu   sync.Mutex
	pauseCond *sync.Cond
	paused    atomic.Bool

	// 节点执行明细收集
	nodeExecMu     sync.Mutex
	nodeExecutions []port.NodeExecution
	nodeStartTimes map[string]time.Time // node_id -> start time
}

// New 创建新的 GraphEngine
func New(
	g *graph.Graph,
	runtimeState *runtime.GraphRuntimeState,
	config *Config,
) *GraphEngine {
	if config == nil {
		config = DefaultConfig()
	}

	eng := &GraphEngine{
		graph:          g,
		runtimeState:   runtimeState,
		config:         config,
		logger:         applog.With("component", "graph_engine"),
		readyQueue:     make(chan string, len(g.Nodes)+1),
		eventQueue:     make(chan event.NodeEvent, 256),
		commandCh:      make(chan types.Command, 16),
		nodeStartTimes: make(map[string]time.Time),
	}
	eng.pauseCond = sync.NewCond(&eng.pauseMu)
	return eng
}

// Run 执行图，返回事件流 channel
func (e *GraphEngine) Run(ctx context.Context) <-chan event.GraphEvent {
	outputCh := make(chan event.GraphEvent, 64)

	go func() {
		defer close(outputCh)

		// 标记开始
		e.runtimeState.Execution().Start()
		outputCh <- event.NewGraphRunStartedEvent()

		// 创建可取消的上下文
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		e.runtimeState.Cancel = cancel

		// 将变量池注入上下文（供节点使用）
		ctx = context.WithValue(ctx, node.ContextKeyVariablePool, e.runtimeState.VariablePool)

		// 检查根节点
		rootNode := e.graph.RootNode
		if rootNode.State() == types.NodeStateSkipped {
			outputCh <- event.NewGraphRunFailedEvent("root node is skipped", 0)
			return
		}

		// 入队根节点（先增加计数再入队）
		e.pendingCount.Add(1)
		e.readyQueue <- rootNode.ID()

		// 启动命令处理器
		go e.commandHandler(cancel)

		// 启动工作协程池
		var workerWg sync.WaitGroup
		for i := 0; i < e.config.MaxWorkers; i++ {
			workerWg.Add(1)
			go e.worker(ctx, &workerWg)
		}

		// 启动事件分发器
		var dispatcherDone sync.WaitGroup
		dispatcherDone.Add(1)
		go func() {
			defer dispatcherDone.Done()
			e.dispatcher(ctx, outputCh)
		}()

		// 等待所有工作完成
		workerWg.Wait()
		close(e.eventQueue)
		dispatcherDone.Wait()

		// 生成最终事件（附带节点执行明细）
		nodeExecs := e.GetNodeExecutions()
		execution := e.runtimeState.Execution()
		if execution.IsAborted() {
			abortEvt := event.NewGraphRunAbortedEvent("workflow execution aborted")
			abortEvt.NodeExecutions = nodeExecs
			outputCh <- abortEvt
		} else if execution.HasError() {
			errMsg := "unknown error"
			if execution.Error != nil {
				errMsg = execution.Error.Error()
			}
			failEvt := event.NewGraphRunFailedEvent(errMsg, execution.ExceptionsCount)
			failEvt.NodeExecutions = nodeExecs
			outputCh <- failEvt
		} else {
			outputs := e.runtimeState.GetOutputs()
			successEvt := event.NewGraphRunSucceededEvent(outputs)
			successEvt.NodeExecutions = nodeExecs
			outputCh <- successEvt
		}

		execution.Complete()
	}()

	return outputCh
}

// SendCommand 发送控制命令到引擎
func (e *GraphEngine) SendCommand(cmd types.Command) {
	select {
	case e.commandCh <- cmd:
	default:
		e.logger.Warn("command channel full, command dropped", "command", cmd.Type)
	}
}

// commandHandler 命令处理器
func (e *GraphEngine) commandHandler(cancel context.CancelFunc) {
	for cmd := range e.commandCh {
		switch cmd.Type {
		case types.CommandAbort:
			e.logger.Info("received abort command", "reason", cmd.Reason)
			e.runtimeState.Execution().Abort(cmd.Reason)
			cancel()

		case types.CommandPause:
			e.logger.Info("received pause command", "reason", cmd.Reason)
			e.paused.Store(true)
			e.runtimeState.Execution().Pause()

		case types.CommandResume:
			e.logger.Info("received resume command")
			e.paused.Store(false)
			e.runtimeState.Execution().Resume()
			e.pauseCond.Broadcast()
		}
	}
}

// checkPaused 阻塞等待直到恢复（如果已暂停）
func (e *GraphEngine) checkPaused(ctx context.Context) bool {
	if !e.paused.Load() {
		return true
	}

	e.pauseMu.Lock()
	for e.paused.Load() {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			e.pauseMu.Unlock()
			return false
		default:
		}
		e.pauseCond.Wait()
	}
	e.pauseMu.Unlock()
	return true
}

// worker 工作协程，从就绪队列取出节点并执行
func (e *GraphEngine) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case nodeID, ok := <-e.readyQueue:
			if !ok {
				return
			}

			// 暂停检查
			if !e.checkPaused(ctx) {
				e.nodeFinished()
				return
			}

			// 检查是否已执行过（去重）
			if _, loaded := e.completedNodes.LoadOrStore(nodeID, true); loaded {
				e.nodeFinished()
				continue
			}

			e.executeNode(ctx, nodeID)
			e.nodeFinished()
		}
	}
}

// enqueueNode 安全地将节点加入就绪队列
func (e *GraphEngine) enqueueNode(nodeID string) {
	e.pendingCount.Add(1)
	e.readyQueue <- nodeID
}

// nodeFinished 标记一个节点处理完毕，如果没有更多待处理节点则关闭队列
func (e *GraphEngine) nodeFinished() {
	remaining := e.pendingCount.Add(-1)
	if remaining <= 0 {
		e.closeOnce.Do(func() {
			close(e.readyQueue)
		})
	}
}

// executeNode 执行单个节点（含错误策略处理）
func (e *GraphEngine) executeNode(ctx context.Context, nodeID string) {
	n, ok := e.graph.Nodes[nodeID]
	if !ok {
		e.logger.Error("node not found", "node_id", nodeID)
		return
	}

	// 跳过已标记为 SKIPPED 的节点
	if n.State() == types.NodeStateSkipped {
		e.logger.Debug("skipping node", "node_id", nodeID, "node_type", n.Type())
		e.processEdges(ctx, nodeID, nil, false)
		return
	}

	// 检查最大步数
	if int(e.runtimeState.NodeRunSteps.Load()) >= e.config.MaxNodeSteps {
		e.logger.Error("max node steps exceeded", "node_id", nodeID)
		e.runtimeState.Execution().Fail(fmt.Errorf("max node steps (%d) exceeded", e.config.MaxNodeSteps))
		return
	}

	e.runtimeState.IncrementNodeRunSteps()
	n.SetState(types.NodeStateTaken)

	// 确定重试次数
	maxAttempts := 1
	retryInterval := time.Duration(0)
	if n.ErrorStrategy() == types.ErrorStrategyRetry {
		if rc := n.RetryConfig(); rc != nil && rc.MaxRetries > 0 {
			maxAttempts = rc.MaxRetries + 1
			retryInterval = time.Duration(rc.RetryInterval) * time.Millisecond
		}
	}

	var lastOutputs map[string]interface{}
	var nodeErr string
	var succeeded bool

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			e.logger.Info("retrying node", "node_id", nodeID, "attempt", attempt+1, "max", maxAttempts)
			if retryInterval > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(retryInterval):
				}
			}
		}

		outputs, errMsg, ok := e.runNodeOnce(ctx, nodeID, n)
		if ok {
			lastOutputs = outputs
			succeeded = true
			break
		}
		nodeErr = errMsg
	}

	if !succeeded {
		// 根据错误策略处理
		switch n.ErrorStrategy() {
		case types.ErrorStrategyFailBranch:
			// 以失败状态继续执行下游 (通过 fail-branch edge)
			e.logger.Info("node failed, following fail-branch", "node_id", nodeID)
			e.eventQueue <- event.NewNodeRunFailedEvent("", nodeID, n.Type(), nodeErr)
			e.runtimeState.VariablePool.SetNodeOutputs(nodeID, map[string]interface{}{
				"__error__": nodeErr,
			})
			e.processEdges(ctx, nodeID, nil, true) // failed=true
			return

		case types.ErrorStrategyDefaultValue:
			// 使用默认值作为输出
			e.logger.Info("node failed, using default value", "node_id", nodeID)
			defaultOutputs := n.DefaultValue()
			if defaultOutputs == nil {
				defaultOutputs = make(map[string]interface{})
			}
			e.runtimeState.VariablePool.SetNodeOutputs(nodeID, defaultOutputs)

			// 发送成功事件（带默认值）
			execID := node.GenerateExecutionID()
			successEvt := event.NewNodeRunSucceededEvent(execID, nodeID, n.Type(), defaultOutputs)
			successEvt.Metadata = map[string]interface{}{"used_default_value": true}
			e.eventQueue <- successEvt
			e.processEdges(ctx, nodeID, defaultOutputs, false)
			return

		default:
			// 无策略或 retry 已耗尽：报错
			e.logger.Error("node failed", "node_id", nodeID, "error", nodeErr)
			e.runtimeState.Execution().Fail(fmt.Errorf("node %s failed: %s", nodeID, nodeErr))
			e.eventQueue <- event.NewNodeRunFailedEvent("", nodeID, n.Type(), nodeErr)
			return
		}
	}

	// 成功：将输出写入变量池
	if lastOutputs != nil {
		e.runtimeState.VariablePool.SetNodeOutputs(nodeID, lastOutputs)
	}

	e.processEdges(ctx, nodeID, lastOutputs, false)
}

// runNodeOnce 执行一次节点，返回 (outputs, errMsg, success)
// 注意：NodeRunFailed 事件不会在此转发，由上层策略处理器决定如何处理
func (e *GraphEngine) runNodeOnce(ctx context.Context, nodeID string, n node.Node) (map[string]interface{}, string, bool) {
	// 创建带超时的上下文
	nodeCtx, nodeCancel := context.WithTimeout(ctx, e.config.NodeTimeout)
	defer nodeCancel()

	// 执行节点
	eventCh, err := n.Run(nodeCtx)
	if err != nil {
		return nil, err.Error(), false
	}

	// 收集节点事件
	var lastOutputs map[string]interface{}
	var failed bool
	var failErr string

	for evt := range eventCh {
		switch evt.Type {
		case event.EventTypeNodeRunFailed:
			// 不转发失败事件，由上层策略处理器负责
			failed = true
			failErr = evt.Error
		default:
			// 转发其他事件（started, succeeded, stream chunk）
			e.eventQueue <- evt
			if evt.Type == event.EventTypeNodeRunSucceeded && evt.Outputs != nil {
				lastOutputs = evt.Outputs
			}
		}
	}

	if failed {
		return nil, failErr, false
	}

	return lastOutputs, "", true
}

// processEdges 处理节点的出边，确定并入队后续节点
// nodeFailed 标记该节点是否以失败状态到达（用于 fail-branch）
func (e *GraphEngine) processEdges(ctx context.Context, nodeID string, outputs map[string]interface{}, nodeFailed bool) {
	outEdges := e.graph.GetOutgoingEdges(nodeID)

	for _, edge := range outEdges {
		if edge.GetState() == types.NodeStateSkipped {
			continue
		}

		targetNodeID := edge.Head
		targetNode, ok := e.graph.Nodes[targetNodeID]
		if !ok {
			continue
		}

		// 处理条件分支
		if !e.shouldFollowEdge(edge, outputs, nodeFailed) {
			edge.SetState(types.NodeStateSkipped)
			e.propagateSkip(targetNodeID)
			continue
		}

		// 检查目标节点的所有入边是否满足（合流）
		if !e.allRequiredInEdgesReady(targetNodeID) {
			continue
		}

		edge.SetState(types.NodeStateTaken)

		// 入队目标节点
		if targetNode.State() != types.NodeStateSkipped {
			e.enqueueNode(targetNodeID)
		}
	}
}

// shouldFollowEdge 判断是否应该走这条边（条件分支判断）
func (e *GraphEngine) shouldFollowEdge(edge *graph.Edge, outputs map[string]interface{}, nodeFailed bool) bool {
	// 普通连接 (source handle = "source")
	if edge.SourceHandle == "source" {
		return !nodeFailed // 正常连接仅在成功时走
	}

	// fail-branch / success-branch
	if edge.SourceHandle == string(types.FailBranchSourceHandleFailed) {
		return nodeFailed // 仅在失败时走 fail-branch
	}
	if edge.SourceHandle == string(types.FailBranchSourceHandleSuccess) {
		return !nodeFailed // 仅在成功时走 success-branch
	}

	// IF/ELSE 条件分支 handle
	if outputs != nil {
		if branch, ok := outputs["__branch__"]; ok {
			return fmt.Sprintf("%v", branch) == edge.SourceHandle
		}
	}

	return true
}

// allRequiredInEdgesReady 检查目标节点的所有必要入边是否已就绪（合流检查）
func (e *GraphEngine) allRequiredInEdgesReady(nodeID string) bool {
	inEdges := e.graph.GetIncomingEdges(nodeID)
	if len(inEdges) <= 1 {
		return true
	}

	for _, edge := range inEdges {
		if edge.GetState() == types.NodeStateSkipped {
			continue
		}
		if _, completed := e.completedNodes.Load(edge.Tail); !completed {
			return false
		}
	}
	return true
}

// propagateSkip 向下游传播 SKIPPED 状态
func (e *GraphEngine) propagateSkip(nodeID string) {
	targetNode, ok := e.graph.Nodes[nodeID]
	if !ok {
		return
	}

	inEdges := e.graph.GetIncomingEdges(nodeID)
	allSkipped := true
	for _, edge := range inEdges {
		if edge.GetState() != types.NodeStateSkipped {
			allSkipped = false
			break
		}
	}

	if !allSkipped {
		return
	}

	targetNode.SetState(types.NodeStateSkipped)

	for _, outEdge := range e.graph.GetOutgoingEdges(nodeID) {
		outEdge.SetState(types.NodeStateSkipped)
		e.propagateSkip(outEdge.Head)
	}
}

// dispatcher 事件分发器
func (e *GraphEngine) dispatcher(ctx context.Context, outputCh chan<- event.GraphEvent) {
	for evt := range e.eventQueue {
		select {
		case <-ctx.Done():
			return
		default:
		}

		graphEvt := event.GraphEvent{
			Type:   evt.Type,
			NodeID: evt.NodeID,
		}

		switch evt.Type {
		case event.EventTypeNodeRunStarted:
			e.logger.Debug("node started",
				"node_id", evt.NodeID,
				"node_type", evt.NodeType,
				"title", evt.NodeTitle,
			)
			// 记录节点开始时间
			e.nodeExecMu.Lock()
			e.nodeStartTimes[evt.NodeID] = evt.StartAt
			e.nodeExecMu.Unlock()

		case event.EventTypeNodeRunSucceeded:
			e.logger.Debug("node succeeded",
				"node_id", evt.NodeID,
				"node_type", evt.NodeType,
			)

			// Response 节点的输出作为工作流最终输出
			n, ok := e.graph.Nodes[evt.NodeID]
			if ok && n.ExecutionType() == types.NodeExecutionTypeResponse {
				if evt.Outputs != nil {
					e.runtimeState.UpdateOutputs(evt.Outputs)
				}
			}

			// 记录节点执行成功
			e.addNodeExec(evt, "succeeded")

		case event.EventTypeNodeRunFailed:
			e.logger.Error("node failed",
				"node_id", evt.NodeID,
				"node_type", evt.NodeType,
				"error", evt.Error,
			)
			e.runtimeState.Execution().IncrementExceptions()
			graphEvt.Error = evt.Error

			// 记录节点执行失败
			e.addNodeExec(evt, "failed")

		case event.EventTypeNodeStreamChunk:
			graphEvt.Chunk = evt.Chunk
		}

		outputCh <- graphEvt
	}
}

// addNodeExec 记录节点执行明细
func (e *GraphEngine) addNodeExec(evt event.NodeEvent, status string) {
	e.nodeExecMu.Lock()
	defer e.nodeExecMu.Unlock()

	startTime, ok := e.nodeStartTimes[evt.NodeID]
	if !ok {
		startTime = time.Now()
	}
	elapsedMs := time.Since(startTime).Milliseconds()

	exec := port.NodeExecution{
		NodeID:    evt.NodeID,
		NodeType:  string(evt.NodeType),
		Title:     evt.NodeTitle,
		Status:    status,
		Outputs:   evt.Outputs,
		Error:     evt.Error,
		StartedAt: startTime,
		ElapsedMs: elapsedMs,
		Metadata:  evt.Metadata,
	}

	e.nodeExecutions = append(e.nodeExecutions, exec)
}

// GetNodeExecutions 获取所有节点执行明细
func (e *GraphEngine) GetNodeExecutions() []port.NodeExecution {
	e.nodeExecMu.Lock()
	defer e.nodeExecMu.Unlock()
	result := make([]port.NodeExecution, len(e.nodeExecutions))
	copy(result, e.nodeExecutions)
	return result
}

// Abort 中止执行
func (e *GraphEngine) Abort() {
	e.SendCommand(types.Command{Type: types.CommandAbort, Reason: "aborted by user"})
}

// Pause 暂停执行
func (e *GraphEngine) Pause() {
	e.SendCommand(types.Command{Type: types.CommandPause, Reason: "paused by user"})
}

// Resume 恢复执行
func (e *GraphEngine) Resume() {
	e.SendCommand(types.Command{Type: types.CommandResume})
}
