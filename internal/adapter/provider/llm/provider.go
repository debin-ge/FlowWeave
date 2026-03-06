package provider

import (
	"context"
)

// Message LLM 对话消息
type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool 角色时必须
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant 角色时可能携带
	Name       string     `json:"name,omitempty"`         // tool 角色时的工具名称
}

// CompletionRequest LLM 补全请求
type CompletionRequest struct {
	Model       string                 `json:"model"`
	Messages    []Message              `json:"messages"`
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	TopP        float64                `json:"top_p,omitempty"`
	Stop        []string               `json:"stop,omitempty"`
	Tools       []ToolDefinition       `json:"tools,omitempty"`       // 工具定义列表
	ToolChoice  interface{}            `json:"tool_choice,omitempty"` // "auto" | "none" | specific
	Extra       map[string]interface{} `json:"extra,omitempty"`       // 供应商特定参数
}

// CompletionResponse LLM 补全响应
type CompletionResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Model        string     `json:"model"`
	FinishReason string     `json:"finish_reason"`
	Usage        Usage      `json:"usage"`
}

// CompletionChunk 流式输出的单个 chunk
type CompletionChunk struct {
	Delta        string     `json:"delta"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// Usage Token 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ToolDefinition OpenAI Function Calling 工具定义
type ToolDefinition struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction 工具函数描述
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema
}

// ToolCall LLM 返回的工具调用请求
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 工具调用函数详情
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// LLMProvider LLM 供应商接口
type LLMProvider interface {
	// Name 返回供应商名称
	Name() string

	// Complete 非流式补全
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// StreamComplete 流式补全，通过 channel 返回 chunks
	StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan CompletionChunk, <-chan error)
}
