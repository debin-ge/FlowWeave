package memory

import (
	"context"
	"time"
)

// MidTermMemory 中期记忆接口
// 存储会话的摘要信息，由 LLM 对溢出短期记忆的对话进行压缩生成
type MidTermMemory interface {
	// LoadSummary 加载会话摘要
	LoadSummary(ctx context.Context, conversationID string) (*ConversationSummary, error)

	// SaveSummary 保存/更新会话摘要
	SaveSummary(ctx context.Context, conversationID string, summary *ConversationSummary) error
}

// ConversationSummary 会话摘要
type ConversationSummary struct {
	Content      string    `json:"content"`       // 摘要文本
	TurnsCovered int       `json:"turns_covered"` // 已覆盖的轮次数
	UpdatedAt    time.Time `json:"updated_at"`    // 最后更新时间
}

// SummaryGenerator 摘要生成器接口
type SummaryGenerator interface {
	// Summarize 根据对话消息生成/更新摘要
	// existingSummary 为已有摘要（增量模式），为空则全新生成
	Summarize(ctx context.Context, messages []Message, existingSummary string) (string, error)
}

// Message 摘要用的简化消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
