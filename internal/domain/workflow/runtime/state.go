package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// GraphRuntimeState 工作流图执行的运行时状态
// 在所有执行组件间共享
type GraphRuntimeState struct {
	mu sync.RWMutex

	// 变量池
	VariablePool *VariablePool

	// 执行时间
	StartAt time.Time

	// Token 统计
	TotalTokens atomic.Int64

	// 节点运行步数
	NodeRunSteps atomic.Int32

	// 最终输出
	outputs   map[string]interface{}
	outputsMu sync.RWMutex

	// 取消函数
	Cancel context.CancelFunc

	// 执行状态
	execution *GraphExecution
}

// NewGraphRuntimeState 创建新的运行时状态
func NewGraphRuntimeState(variablePool *VariablePool) *GraphRuntimeState {
	return &GraphRuntimeState{
		VariablePool: variablePool,
		StartAt:      time.Now(),
		outputs:      make(map[string]interface{}),
		execution:    NewGraphExecution(),
	}
}

// Execution 获取执行聚合体
func (s *GraphRuntimeState) Execution() *GraphExecution {
	return s.execution
}

// SetOutput 设置工作流输出
func (s *GraphRuntimeState) SetOutput(key string, value interface{}) {
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	s.outputs[key] = value
}

// GetOutputs 获取所有输出的副本
func (s *GraphRuntimeState) GetOutputs() map[string]interface{} {
	s.outputsMu.RLock()
	defer s.outputsMu.RUnlock()
	result := make(map[string]interface{}, len(s.outputs))
	for k, v := range s.outputs {
		result[k] = v
	}
	return result
}

// UpdateOutputs 批量更新输出
func (s *GraphRuntimeState) UpdateOutputs(outputs map[string]interface{}) {
	s.outputsMu.Lock()
	defer s.outputsMu.Unlock()
	for k, v := range outputs {
		s.outputs[k] = v
	}
}

// AddTokens 增加 token 计数
func (s *GraphRuntimeState) AddTokens(tokens int64) {
	s.TotalTokens.Add(tokens)
}

// IncrementNodeRunSteps 增加节点运行步数
func (s *GraphRuntimeState) IncrementNodeRunSteps() {
	s.NodeRunSteps.Add(1)
}

// Dump 序列化运行时状态为 JSON
func (s *GraphRuntimeState) Dump() ([]byte, error) {
	s.outputsMu.RLock()
	outputsCopy := make(map[string]interface{}, len(s.outputs))
	for k, v := range s.outputs {
		outputsCopy[k] = v
	}
	s.outputsMu.RUnlock()

	snapshot := map[string]interface{}{
		"version":        "1.0",
		"start_at":       s.StartAt.UnixMilli(),
		"total_tokens":   s.TotalTokens.Load(),
		"node_run_steps": s.NodeRunSteps.Load(),
		"outputs":        outputsCopy,
		"variable_pool":  s.VariablePool.Dump(),
		"execution":      s.execution.Dump(),
	}

	return json.Marshal(snapshot)
}

// GraphExecution 图执行聚合体，跟踪整体执行状态
type GraphExecution struct {
	mu sync.RWMutex

	WorkflowID      string
	Started         bool
	Completed       bool
	Aborted         bool
	Paused          bool
	Error           error
	ExceptionsCount int
}

// NewGraphExecution 创建新的执行聚合体
func NewGraphExecution() *GraphExecution {
	return &GraphExecution{}
}

// Start 标记执行开始
func (e *GraphExecution) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Started = true
}

// Complete 标记执行完成
func (e *GraphExecution) Complete() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Completed = true
}

// Abort 中止执行
func (e *GraphExecution) Abort(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Aborted = true
	if reason != "" {
		e.Error = fmt.Errorf("aborted: %s", reason)
	}
}

// Pause 暂停执行
func (e *GraphExecution) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Paused = true
}

// Resume 恢复执行
func (e *GraphExecution) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Paused = false
}

// Fail 记录不可恢复的错误
func (e *GraphExecution) Fail(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Error = err
	e.Completed = true
}

// IncrementExceptions 增加异常计数
func (e *GraphExecution) IncrementExceptions() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ExceptionsCount++
}

// HasError 是否有错误
func (e *GraphExecution) HasError() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Error != nil
}

// IsCompleted 是否已完成
func (e *GraphExecution) IsCompleted() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Completed
}

// IsAborted 是否已中止
func (e *GraphExecution) IsAborted() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Aborted
}

// IsPaused 是否已暂停
func (e *GraphExecution) IsPaused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Paused
}

// Dump 序列化执行状态
func (e *GraphExecution) Dump() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	errStr := ""
	if e.Error != nil {
		errStr = e.Error.Error()
	}

	return map[string]interface{}{
		"workflow_id":      e.WorkflowID,
		"started":          e.Started,
		"completed":        e.Completed,
		"aborted":          e.Aborted,
		"paused":           e.Paused,
		"error":            errStr,
		"exceptions_count": e.ExceptionsCount,
	}
}
