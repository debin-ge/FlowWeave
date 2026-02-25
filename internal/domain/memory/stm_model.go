package memory

import (
	applog "flowweave/internal/platform/log"

	"flowweave/internal/provider"
)

// --- Gateway 全局默认值（从环境变量设置，可被 DSL 覆盖）---

var (
	defaultContextWindowSize   = 128000
	defaultTokenThresholdRatio = 0.70
	defaultMinRecentTurns      = 4
)

// SetGatewayDefaults 设置 Gateway 全局默认值（从环境变量读取）
func SetGatewayDefaults(contextWindowSize int, thresholdRatio float64, minRecentTurns int) {
	if contextWindowSize > 0 {
		defaultContextWindowSize = contextWindowSize
	}
	if thresholdRatio > 0 {
		defaultTokenThresholdRatio = thresholdRatio
	}
	if minRecentTurns > 0 {
		defaultMinRecentTurns = minRecentTurns
	}
	applog.Info("[Gateway] Defaults updated",
		"context_window_size", defaultContextWindowSize,
		"token_threshold_ratio", defaultTokenThresholdRatio,
		"min_recent_turns", defaultMinRecentTurns,
	)
}

// STMState 增强版 STM 状态（Redis Hash 存储）
type STMState struct {
	SessionID           string             `json:"session_id"`
	RecentMessages      []provider.Message `json:"recent_messages"`
	GatewaySummary      string             `json:"gateway_summary"`
	KeyFacts            map[string]string  `json:"key_facts"`
	TokenEstimate       int                `json:"token_estimate"`
	LastCompressedAt    int64              `json:"last_compressed_at"`
	CompressedTurnCount int                `json:"compressed_turn_count"`
	Version             int                `json:"version"`
}

// GatewayConfig Context-Gateway 压缩配置
type GatewayConfig struct {
	Enabled             bool                `json:"enabled"`
	Model               *SummaryModelConfig `json:"model,omitempty"`
	TokenThresholdRatio float64             `json:"token_threshold_ratio,omitempty"` // Token 阈值比例（默认 0.70）
	MinRecentTurns      int                 `json:"min_recent_turns,omitempty"`      // 最少保留轮次（默认 4）
	ExtractKeyFacts     bool                `json:"extract_key_facts,omitempty"`     // 是否提取 Key Facts
	ContextWindowSize   int                 `json:"context_window_size,omitempty"`   // 模型上下文窗口（默认 128000）
}

// GetTokenThresholdRatio 获取 Token 阈值比例（DSL 优先，否则用环境变量默认值）
func (gc *GatewayConfig) GetTokenThresholdRatio() float64 {
	if gc != nil && gc.TokenThresholdRatio > 0 {
		return gc.TokenThresholdRatio
	}
	return defaultTokenThresholdRatio
}

// GetMinRecentTurns 获取最少保留轮次（DSL 优先，否则用环境变量默认值）
func (gc *GatewayConfig) GetMinRecentTurns() int {
	if gc != nil && gc.MinRecentTurns > 0 {
		return gc.MinRecentTurns
	}
	return defaultMinRecentTurns
}

// GetContextWindowSize 获取上下文窗口大小（DSL 优先，否则用环境变量默认值）
func (gc *GatewayConfig) GetContextWindowSize() int {
	if gc != nil && gc.ContextWindowSize > 0 {
		return gc.ContextWindowSize
	}
	return defaultContextWindowSize
}

// CompressResult Context-Gateway 压缩结果
type CompressResult struct {
	CompressedSummary string            `json:"compressed_summary"`
	KeyFacts          map[string]string `json:"key_facts"`
}
