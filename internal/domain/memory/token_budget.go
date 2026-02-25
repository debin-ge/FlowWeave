package memory

import (
	"flowweave/internal/provider"
)

// TokenEstimator Token 估算器接口
type TokenEstimator interface {
	// EstimateTokens 估算文本的 Token 数
	EstimateTokens(text string) int
}

// SimpleTokenEstimator 简单 Token 估算器
// 英文约 4 字符 ≈ 1 token，中文约 1.5 字符 ≈ 1 token
// 取保守估计：rune 数量 * 2 / 3
type SimpleTokenEstimator struct{}

// EstimateTokens 估算文本的 Token 数
func (e *SimpleTokenEstimator) EstimateTokens(text string) int {
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	return runes * 2 / 3
}

// EstimateMessages 批量估算消息列表的 Token 数
func EstimateMessages(estimator TokenEstimator, messages []provider.Message) int {
	total := 0
	for _, msg := range messages {
		// 每条消息有 role 标记开销（约 4 token）
		total += 4
		total += estimator.EstimateTokens(msg.Content)
	}
	return total
}

// EstimateSTMContext 估算整个 STM 上下文的 Token 数
// 包括 gateway_summary + key_facts + recent_messages
func EstimateSTMContext(estimator TokenEstimator, state *STMState) int {
	total := 0

	// Gateway Summary
	if state.GatewaySummary != "" {
		total += estimator.EstimateTokens(state.GatewaySummary)
		total += 4 // system message overhead
	}

	// Key Facts
	if len(state.KeyFacts) > 0 {
		for k, v := range state.KeyFacts {
			total += estimator.EstimateTokens(k + ": " + v)
		}
		total += 4 // system message overhead
	}

	// Recent Messages
	total += EstimateMessages(estimator, state.RecentMessages)

	return total
}

// ShouldCompress 判断是否应该触发压缩
func ShouldCompress(estimator TokenEstimator, state *STMState, config *GatewayConfig) bool {
	if config == nil || !config.Enabled {
		return false
	}

	contextWindow := config.GetContextWindowSize()
	thresholdRatio := config.GetTokenThresholdRatio()
	threshold := int(float64(contextWindow) * thresholdRatio)

	currentTokens := EstimateSTMContext(estimator, state)
	return currentTokens > threshold
}
