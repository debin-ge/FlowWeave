package port

import (
	"encoding/json"
	"time"
)

// WorkflowStatus 工作流状态
type WorkflowStatus string

const (
	WorkflowStatusActive   WorkflowStatus = "active"
	WorkflowStatusArchived WorkflowStatus = "archived"
)

// RunStatus 执行状态
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusAborted   RunStatus = "aborted"
)

// Workflow 工作流定义模型
type Workflow struct {
	ID          string          `json:"id"`
	OrgID       string          `json:"org_id,omitempty"`
	TenantID    string          `json:"tenant_id,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	DSL         json.RawMessage `json:"dsl"`
	Version     int             `json:"version"`
	Status      WorkflowStatus  `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// WorkflowRun 执行记录模型
type WorkflowRun struct {
	ID             string          `json:"id"`
	WorkflowID     string          `json:"workflow_id"`
	OrgID          string          `json:"org_id,omitempty"`
	TenantID       string          `json:"tenant_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Status         RunStatus       `json:"status"`
	Inputs         json.RawMessage `json:"inputs,omitempty"`
	Outputs        json.RawMessage `json:"outputs,omitempty"`
	Error          string          `json:"error,omitempty"`
	TotalTokens    int             `json:"total_tokens"`
	TotalSteps     int             `json:"total_steps"`
	ElapsedMs      int64           `json:"elapsed_ms"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
}

// NodeExecution 单个节点的执行记录（引擎内部使用）
type NodeExecution struct {
	NodeID    string                 `json:"node_id"`
	NodeType  string                 `json:"node_type"`
	Title     string                 `json:"title,omitempty"`
	Status    string                 `json:"status"` // succeeded / failed / skipped
	Outputs   map[string]interface{} `json:"outputs,omitempty"`
	Error     string                 `json:"error,omitempty"`
	StartedAt time.Time              `json:"started_at"`
	ElapsedMs int64                  `json:"elapsed_ms"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // tokens 等额外信息
}

// NodeExecutionRecord 独立表的节点执行记录
type NodeExecutionRecord struct {
	ID        string                 `json:"id"`
	RunID     string                 `json:"run_id"`
	NodeID    string                 `json:"node_id"`
	NodeType  string                 `json:"node_type"`
	Title     string                 `json:"title,omitempty"`
	Status    string                 `json:"status"`
	Outputs   map[string]interface{} `json:"outputs,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	ElapsedMs int64                  `json:"elapsed_ms"`
	StartedAt time.Time              `json:"started_at"`
}

// ConversationTrace AI Provider 调用溯源记录
type ConversationTrace struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id,omitempty"`
	TenantID       string          `json:"tenant_id,omitempty"`
	ConversationID string          `json:"conversation_id"`
	Traces         json.RawMessage `json:"traces"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// LLMCallTrace 单次 LLM 调用的完整记录
type LLMCallTrace struct {
	Timestamp time.Time         `json:"timestamp"`
	RunID     string            `json:"run_id,omitempty"`
	NodeID    string            `json:"node_id"`
	Provider  string            `json:"provider"`
	Model     string            `json:"model"`
	Request   *LLMTraceRequest  `json:"request"`
	Response  *LLMTraceResponse `json:"response,omitempty"`
	ElapsedMs int64             `json:"elapsed_ms"`
	Error     string            `json:"error,omitempty"`
}

// LLMTraceRequest LLM 请求记录
type LLMTraceRequest struct {
	Messages    []LLMTraceMessage      `json:"messages"`
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	TopP        float64                `json:"top_p,omitempty"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
}

// LLMTraceResponse LLM 响应记录
type LLMTraceResponse struct {
	Content      string         `json:"content"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Usage        *LLMTraceUsage `json:"usage,omitempty"`
}

// LLMTraceMessage 消息记录
type LLMTraceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMTraceUsage Token 使用记录
type LLMTraceUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LLMCallTraceRecord 独立表的 LLM 调用溯源记录
type LLMCallTraceRecord struct {
	ID             string            `json:"id"`
	RunID          string            `json:"run_id,omitempty"`
	ConversationID string            `json:"conversation_id"`
	OrgID          string            `json:"org_id,omitempty"`
	TenantID       string            `json:"tenant_id,omitempty"`
	NodeID         string            `json:"node_id"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	Request        *LLMTraceRequest  `json:"request,omitempty"`
	Response       *LLMTraceResponse `json:"response,omitempty"`
	ElapsedMs      int64             `json:"elapsed_ms"`
	Error          string            `json:"error,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

// ListWorkflowsParams 查询参数
type ListWorkflowsParams struct {
	Page     int
	PageSize int
	Status   WorkflowStatus
	Search   string // 按名称模糊搜索
}

// ListWorkflowsResult 分页结果
type ListWorkflowsResult struct {
	Workflows []*Workflow `json:"workflows"`
	Total     int         `json:"total"`
	Page      int         `json:"page"`
	PageSize  int         `json:"page_size"`
}

// Organization 组织数据模型
type Organization struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // active / inactive
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tenant 租户数据模型
type Tenant struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // active / inactive
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Dataset 知识库数据模型
type Dataset struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // active / inactive
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Document 文档元数据模型
type Document struct {
	ID         string    `json:"id"`
	DatasetID  string    `json:"dataset_id"`
	OrgID      string    `json:"org_id"`
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name"`
	Source     string    `json:"source,omitempty"`
	ChunkCount int       `json:"chunk_count"`
	Status     string    `json:"status"` // processing / completed / failed
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
