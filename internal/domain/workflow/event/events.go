package event

import (
	"time"

	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/port"
)

// EventType 事件类型标识
type EventType string

const (
	// 图级事件
	EventTypeGraphRunStarted          EventType = "graph_run_started"
	EventTypeGraphRunSucceeded        EventType = "graph_run_succeeded"
	EventTypeGraphRunFailed           EventType = "graph_run_failed"
	EventTypeGraphRunAborted          EventType = "graph_run_aborted"
	EventTypeGraphRunPaused           EventType = "graph_run_paused"
	EventTypeGraphRunPartialSucceeded EventType = "graph_run_partial_succeeded"

	// 节点级事件
	EventTypeNodeRunStarted   EventType = "node_run_started"
	EventTypeNodeRunSucceeded EventType = "node_run_succeeded"
	EventTypeNodeRunFailed    EventType = "node_run_failed"
	EventTypeNodeStreamChunk  EventType = "node_stream_chunk"
)

// GraphEvent 图级事件（面向外部消费者的顶层事件）
type GraphEvent struct {
	Type            EventType              `json:"type"`
	NodeID          string                 `json:"node_id,omitempty"`
	Chunk           string                 `json:"chunk,omitempty"`
	Outputs         map[string]interface{} `json:"outputs,omitempty"`
	Error           string                 `json:"error,omitempty"`
	ExceptionsCount int                    `json:"exceptions_count,omitempty"`
	NodeExecutions  []port.NodeExecution   `json:"node_executions,omitempty"`
}

// NewGraphRunStartedEvent 创建图开始执行事件
func NewGraphRunStartedEvent() GraphEvent {
	return GraphEvent{Type: EventTypeGraphRunStarted}
}

// NewGraphRunSucceededEvent 创建图执行成功事件
func NewGraphRunSucceededEvent(outputs map[string]interface{}) GraphEvent {
	return GraphEvent{Type: EventTypeGraphRunSucceeded, Outputs: outputs}
}

// NewGraphRunFailedEvent 创建图执行失败事件
func NewGraphRunFailedEvent(err string, exceptionsCount int) GraphEvent {
	return GraphEvent{Type: EventTypeGraphRunFailed, Error: err, ExceptionsCount: exceptionsCount}
}

// NewGraphRunAbortedEvent 创建图执行中止事件
func NewGraphRunAbortedEvent(reason string) GraphEvent {
	return GraphEvent{Type: EventTypeGraphRunAborted, Error: reason}
}

// NodeEvent 节点级事件（内部节点执行产生的事件）
type NodeEvent struct {
	Type      EventType                 `json:"type"`
	ID        string                    `json:"id"` // 执行 ID
	NodeID    string                    `json:"node_id"`
	NodeType  types.NodeType            `json:"node_type"`
	NodeTitle string                    `json:"node_title,omitempty"`
	StartAt   time.Time                 `json:"start_at,omitempty"`
	Outputs   map[string]interface{}    `json:"outputs,omitempty"`
	Error     string                    `json:"error,omitempty"`
	Status    types.NodeExecutionStatus `json:"status,omitempty"`

	// 流式输出
	Chunk string `json:"chunk,omitempty"`

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// LLM 相关
	TotalTokens int   `json:"total_tokens,omitempty"`
	ElapsedMs   int64 `json:"elapsed_ms,omitempty"`
}

// NewNodeRunStartedEvent 创建节点开始事件
func NewNodeRunStartedEvent(executionID, nodeID string, nodeType types.NodeType, title string) NodeEvent {
	return NodeEvent{
		Type:      EventTypeNodeRunStarted,
		ID:        executionID,
		NodeID:    nodeID,
		NodeType:  nodeType,
		NodeTitle: title,
		StartAt:   time.Now(),
	}
}

// NewNodeRunSucceededEvent 创建节点成功事件
func NewNodeRunSucceededEvent(executionID, nodeID string, nodeType types.NodeType, outputs map[string]interface{}) NodeEvent {
	return NodeEvent{
		Type:     EventTypeNodeRunSucceeded,
		ID:       executionID,
		NodeID:   nodeID,
		NodeType: nodeType,
		Outputs:  outputs,
		Status:   types.NodeExecutionStatusSucceeded,
	}
}

// NewNodeRunFailedEvent 创建节点失败事件
func NewNodeRunFailedEvent(executionID, nodeID string, nodeType types.NodeType, err string) NodeEvent {
	return NodeEvent{
		Type:     EventTypeNodeRunFailed,
		ID:       executionID,
		NodeID:   nodeID,
		NodeType: nodeType,
		Error:    err,
		Status:   types.NodeExecutionStatusFailed,
	}
}

// NewNodeStreamChunkEvent 创建流式输出事件
func NewNodeStreamChunkEvent(executionID, nodeID string, nodeType types.NodeType, chunk string) NodeEvent {
	return NodeEvent{
		Type:     EventTypeNodeStreamChunk,
		ID:       executionID,
		NodeID:   nodeID,
		NodeType: nodeType,
		Chunk:    chunk,
	}
}
