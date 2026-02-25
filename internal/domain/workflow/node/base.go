package node

import (
	"context"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
)

// Node 是所有工作流节点必须实现的接口
type Node interface {
	// ID 返回节点唯一标识
	ID() string

	// Type 返回节点类型
	Type() types.NodeType

	// Title 返回节点标题
	Title() string

	// ExecutionType 返回节点执行类型分类
	ExecutionType() types.NodeExecutionType

	// State 返回节点当前状态
	State() types.NodeState

	// SetState 设置节点状态
	SetState(state types.NodeState)

	// ErrorStrategy 返回节点的错误处理策略
	ErrorStrategy() types.ErrorStrategy

	// DefaultValue 返回 default-value 策略的默认输出
	DefaultValue() map[string]interface{}

	// RetryConfig 返回 retry 策略配置
	RetryConfig() *types.RetryConfig

	// Run 执行节点逻辑，通过 channel 返回事件流
	// ctx 用于传递取消信号和运行时状态
	Run(ctx context.Context) (<-chan event.NodeEvent, error)
}

// NodeRunResult 节点执行结果
type NodeRunResult struct {
	Status   types.NodeExecutionStatus `json:"status"`
	Outputs  map[string]interface{}    `json:"outputs,omitempty"`
	Error    string                    `json:"error,omitempty"`
	Metadata map[string]interface{}    `json:"metadata,omitempty"`
}

// FailedResult 创建失败的执行结果
func FailedResult(errMsg string) *NodeRunResult {
	return &NodeRunResult{
		Status: types.NodeExecutionStatusFailed,
		Error:  errMsg,
	}
}

// VariablePoolAccessor 节点访问变量池的接口
type VariablePoolAccessor interface {
	// GetVariable 通过选择器获取变量值
	GetVariable(selector types.VariableSelector) (interface{}, bool)
	// SetVariable 设置节点输出变量
	SetVariable(nodeID, varName string, value interface{})
}

// RuntimeContext 运行时上下文键
type contextKey string

const (
	// ContextKeyVariablePool 变量池上下文键
	ContextKeyVariablePool contextKey = "variable_pool"
)

// GetVariablePoolFromContext 从 context 中获取变量池
func GetVariablePoolFromContext(ctx context.Context) (VariablePoolAccessor, bool) {
	vp, ok := ctx.Value(ContextKeyVariablePool).(VariablePoolAccessor)
	return vp, ok
}
