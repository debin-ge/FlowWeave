package types

import "encoding/json"

// GraphConfig 图的完整配置，来自 DSL 或数据库
type GraphConfig struct {
	Nodes []NodeConfig `json:"nodes"`
	Edges []EdgeConfig `json:"edges"`
}

// NodeConfig 节点配置
type NodeConfig struct {
	ID   string          `json:"id"`
	Type string          `json:"type,omitempty"`
	Data json.RawMessage `json:"data"`
}

// EdgeConfig 边配置
type EdgeConfig struct {
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries    int `json:"max_retries"`    // 最大重试次数
	RetryInterval int `json:"retry_interval"` // 重试间隔（毫秒）
}

// NodeData 节点数据的通用结构
type NodeData struct {
	Type          string                 `json:"type"`
	Title         string                 `json:"title"`
	Description   string                 `json:"desc,omitempty"`
	ErrorStrategy ErrorStrategy          `json:"error_strategy,omitempty"`
	DefaultValue  map[string]interface{} `json:"default_value,omitempty"` // default-value 策略的默认输出
	Retry         *RetryConfig           `json:"retry,omitempty"`         // retry 策略配置
}

// VariableSelector 变量选择器 [node_id, variable_name]
type VariableSelector []string

// NodeID 返回变量所属节点ID
func (vs VariableSelector) NodeID() string {
	if len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// VarName 返回变量名
func (vs VariableSelector) VarName() string {
	if len(vs) > 1 {
		return vs[1]
	}
	return ""
}
