package memory

import (
	"context"

	"flowweave/internal/provider"
)

// ShortTermMemory 短期记忆接口
// 存储当前会话的原始对话历史，提供滑动窗口管理
type ShortTermMemory interface {
	// Load 加载最近 N 轮对话
	Load(ctx context.Context, conversationID string, windowSize int) ([]provider.Message, error)

	// Append 追加对话消息
	Append(ctx context.Context, conversationID string, msgs ...provider.Message) error

	// GetTurnCount 获取当前轮次数
	GetTurnCount(ctx context.Context, conversationID string) (int, error)

	// Clear 清空会话
	Clear(ctx context.Context, conversationID string) error
}

// MemoryConfig DSL 中 LLM 节点的记忆配置
// 依赖规则：long_term 需同时启用 mid_term + short_term；mid_term 需同时启用 short_term
type MemoryConfig struct {
	ShortTerm *ShortTermConfig `json:"short_term,omitempty"`
	MidTerm   *MidTermConfig   `json:"mid_term,omitempty"`
	LongTerm  *LongTermConfig  `json:"long_term,omitempty"`
}

// ShortTermConfig 短期记忆配置
type ShortTermConfig struct {
	Enabled         bool           `json:"enabled"`
	WindowSize      int            `json:"window_size,omitempty"`      // 默认 20
	GatewayCompress *GatewayConfig `json:"gateway_compress,omitempty"` // Context-Gateway 压缩配置
}

// MidTermConfig 中期记忆配置
type MidTermConfig struct {
	Enabled          bool                `json:"enabled"`
	SummaryModel     *SummaryModelConfig `json:"summary_model,omitempty"`     // 摘要用 LLM，不配置则复用当前模型
	SummaryThreshold int                 `json:"summary_threshold,omitempty"` // 超此轮次触发摘要，默认 10
}

// SummaryModelConfig 摘要 LLM 模型配置
type SummaryModelConfig struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// GetSummaryThreshold 获取摘要触发阈值
func (mc *MidTermConfig) GetSummaryThreshold() int {
	if mc != nil && mc.SummaryThreshold > 0 {
		return mc.SummaryThreshold
	}
	return 10 // 默认 10 轮
}

// LongTermConfig 长期记忆配置（预留）
type LongTermConfig struct {
	Enabled bool `json:"enabled"`
}

// IsShortTermEnabled 短期记忆是否启用
func (mc *MemoryConfig) IsShortTermEnabled() bool {
	return mc != nil && mc.ShortTerm != nil && mc.ShortTerm.Enabled
}

// IsGatewayEnabled Gateway 压缩是否启用
func (mc *MemoryConfig) IsGatewayEnabled() bool {
	return mc.IsShortTermEnabled() && mc.ShortTerm.GatewayCompress != nil && mc.ShortTerm.GatewayCompress.Enabled
}

// GetGatewayConfig 获取 Gateway 配置
func (mc *MemoryConfig) GetGatewayConfig() *GatewayConfig {
	if mc != nil && mc.ShortTerm != nil {
		return mc.ShortTerm.GatewayCompress
	}
	return nil
}

// IsMidTermEnabled 中期记忆是否启用
func (mc *MemoryConfig) IsMidTermEnabled() bool {
	return mc != nil && mc.MidTerm != nil && mc.MidTerm.Enabled
}

// IsLongTermEnabled 长期记忆是否启用
func (mc *MemoryConfig) IsLongTermEnabled() bool {
	return mc != nil && mc.LongTerm != nil && mc.LongTerm.Enabled
}

// GetWindowSize 获取短期记忆窗口大小
func (mc *MemoryConfig) GetWindowSize() int {
	if mc != nil && mc.ShortTerm != nil && mc.ShortTerm.WindowSize > 0 {
		return mc.ShortTerm.WindowSize
	}
	return 20 // 默认 20 轮
}

// Validate 验证记忆配置的依赖关系
func (mc *MemoryConfig) Validate() error {
	if mc == nil {
		return nil
	}

	// 中期记忆需要短期记忆
	if mc.IsMidTermEnabled() && !mc.IsShortTermEnabled() {
		return ErrMidTermRequiresShortTerm
	}

	// 长期记忆需要中期+短期
	if mc.IsLongTermEnabled() && !mc.IsMidTermEnabled() {
		return ErrLongTermRequiresMidTerm
	}

	return nil
}
