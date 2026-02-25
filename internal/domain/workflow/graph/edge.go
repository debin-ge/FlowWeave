package graph

import (
	"sync"

	types "flowweave/internal/domain/workflow/model"

	"github.com/google/uuid"
)

// Edge 表示工作流图中两个节点之间的连接
type Edge struct {
	ID           string `json:"id"`
	Tail         string `json:"tail"`          // 源节点 ID
	Head         string `json:"head"`          // 目标节点 ID
	SourceHandle string `json:"source_handle"` // 用于条件分支的 handle

	mu    sync.RWMutex
	state types.NodeState
}

// GetState 线程安全地获取边状态
func (e *Edge) GetState() types.NodeState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// SetState 线程安全地设置边状态
func (e *Edge) SetState(s types.NodeState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = s
}

// NewEdge 创建一个新的 Edge
func NewEdge(tail, head, sourceHandle string) *Edge {
	return &Edge{
		ID:           uuid.New().String(),
		Tail:         tail,
		Head:         head,
		SourceHandle: sourceHandle,
		state:        types.NodeStateUnknown,
	}
}

// NewEdgeWithID 创建一个带指定 ID 的 Edge
func NewEdgeWithID(id, tail, head, sourceHandle string) *Edge {
	return &Edge{
		ID:           id,
		Tail:         tail,
		Head:         head,
		SourceHandle: sourceHandle,
		state:        types.NodeStateUnknown,
	}
}
