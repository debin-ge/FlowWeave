package node

import (
	"context"
	"fmt"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"

	"github.com/google/uuid"
)

// BaseNode 提供 Node 接口的公共实现基础
type BaseNode struct {
	id            string
	nodeType      types.NodeType
	title         string
	executionType types.NodeExecutionType
	state         types.NodeState
	errorStrategy types.ErrorStrategy
	defaultValue  map[string]interface{}
	retryConfig   *types.RetryConfig
	rawData       map[string]interface{}
}

// NewBaseNode 创建 BaseNode
func NewBaseNode(id string, nodeType types.NodeType, title string, executionType types.NodeExecutionType) *BaseNode {
	return &BaseNode{
		id:            id,
		nodeType:      nodeType,
		title:         title,
		executionType: executionType,
		state:         types.NodeStateUnknown,
		errorStrategy: types.ErrorStrategyNone,
	}
}

func (n *BaseNode) ID() string                             { return n.id }
func (n *BaseNode) Type() types.NodeType                   { return n.nodeType }
func (n *BaseNode) Title() string                          { return n.title }
func (n *BaseNode) ExecutionType() types.NodeExecutionType { return n.executionType }
func (n *BaseNode) State() types.NodeState                 { return n.state }
func (n *BaseNode) SetState(state types.NodeState)         { n.state = state }
func (n *BaseNode) ErrorStrategy() types.ErrorStrategy     { return n.errorStrategy }
func (n *BaseNode) DefaultValue() map[string]interface{}   { return n.defaultValue }
func (n *BaseNode) RetryConfig() *types.RetryConfig        { return n.retryConfig }

// SetErrorStrategy 设置错误策略
func (n *BaseNode) SetErrorStrategy(strategy types.ErrorStrategy) {
	n.errorStrategy = strategy
}

// SetDefaultValue 设置默认值
func (n *BaseNode) SetDefaultValue(dv map[string]interface{}) {
	n.defaultValue = dv
}

// SetRetryConfig 设置重试配置
func (n *BaseNode) SetRetryConfig(rc *types.RetryConfig) {
	n.retryConfig = rc
}

// SetRawData 设置原始配置数据
func (n *BaseNode) SetRawData(data map[string]interface{}) {
	n.rawData = data
}

// RawData 获取原始配置数据
func (n *BaseNode) RawData() map[string]interface{} {
	return n.rawData
}

// GenerateExecutionID 生成节点执行 ID
func GenerateExecutionID() string {
	return uuid.New().String()
}

// RunWithEvents 通用的事件包装执行逻辑
// 子类实现 executor 函数提供具体的执行逻辑
func RunWithEvents(
	ctx context.Context,
	n Node,
	executor func(ctx context.Context) (*NodeRunResult, error),
) (<-chan event.NodeEvent, error) {
	ch := make(chan event.NodeEvent, 16)

	go func() {
		defer close(ch)

		executionID := GenerateExecutionID()

		// 发送开始事件
		ch <- event.NewNodeRunStartedEvent(executionID, n.ID(), n.Type(), n.Title())

		// 执行节点逻辑
		result, err := executor(ctx)
		if err != nil {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(), err.Error())
			return
		}

		if result == nil {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(), "node returned nil result")
			return
		}

		if result.Status == types.NodeExecutionStatusFailed {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(), result.Error)
			return
		}

		// 发送成功事件
		successEvent := event.NewNodeRunSucceededEvent(executionID, n.ID(), n.Type(), result.Outputs)
		successEvent.Metadata = result.Metadata
		ch <- successEvent
	}()

	return ch, nil
}

// RunStreamWithEvents 支持流式输出的事件包装执行逻辑
func RunStreamWithEvents(
	ctx context.Context,
	n Node,
	executor func(ctx context.Context, stream chan<- string) (*NodeRunResult, error),
) (<-chan event.NodeEvent, error) {
	ch := make(chan event.NodeEvent, 64)

	go func() {
		defer close(ch)

		executionID := GenerateExecutionID()

		// 发送开始事件
		ch <- event.NewNodeRunStartedEvent(executionID, n.ID(), n.Type(), n.Title())

		// 创建流式输出通道
		streamCh := make(chan string, 32)
		doneCh := make(chan struct{})

		var result *NodeRunResult
		var execErr error

		// 在独立 goroutine 中执行节点逻辑
		go func() {
			defer close(streamCh)
			result, execErr = executor(ctx, streamCh)
			close(doneCh)
		}()

		// 转发流式输出为事件
		for chunk := range streamCh {
			ch <- event.NewNodeStreamChunkEvent(executionID, n.ID(), n.Type(), chunk)
		}

		// 等待执行完成
		<-doneCh

		if execErr != nil {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(), execErr.Error())
			return
		}

		if result == nil {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(), "node returned nil result")
			return
		}

		if result.Status == types.NodeExecutionStatusFailed {
			ch <- event.NewNodeRunFailedEvent(executionID, n.ID(), n.Type(),
				fmt.Sprintf("node execution failed: %s", result.Error))
			return
		}

		successEvent := event.NewNodeRunSucceededEvent(executionID, n.ID(), n.Type(), result.Outputs)
		successEvent.Metadata = result.Metadata
		ch <- successEvent
	}()

	return ch, nil
}
