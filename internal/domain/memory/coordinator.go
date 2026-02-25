package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"reflect"
	"sync"
	"time"

	"flowweave/internal/domain/workflow/port"
	"flowweave/internal/provider"
)

// Coordinator 记忆协调器，编排三层记忆的加载和存储
type Coordinator struct {
	shortTerm    ShortTermMemory
	midTerm      MidTermMemory      // 中期记忆（可为 nil）
	summaryGen   SummaryGenerator   // 摘要生成器（可为 nil）
	gateway      *GatewayCompressor // Context-Gateway 压缩器（可为 nil）
	compressLock *RedisCompressLock // 分布式压缩锁（可为 nil）
	// longTerm  LongTermMemory // 预留
}

// NewCoordinator 创建记忆协调器
func NewCoordinator(stm ShortTermMemory) *Coordinator {
	return &Coordinator{
		shortTerm: stm,
	}
}

// WithMidTerm 设置中期记忆（链式调用）
func (c *Coordinator) WithMidTerm(mtm MidTermMemory, gen SummaryGenerator) *Coordinator {
	c.midTerm = mtm
	c.summaryGen = gen
	applog.Info("[Memory/Coordinator] Mid-term memory configured",
		"has_mtm", mtm != nil,
		"has_summary_gen", gen != nil,
	)
	return c
}

// WithGateway 设置 Gateway 压缩器和分布式锁（链式调用）
func (c *Coordinator) WithGateway(gw *GatewayCompressor, lock *RedisCompressLock) *Coordinator {
	c.gateway = gw
	c.compressLock = lock
	applog.Info("[Memory/Coordinator] Gateway compressor configured",
		"has_gateway", gw != nil,
		"has_lock", lock != nil,
	)
	return c
}

// RecallRequest 回忆请求
type RecallRequest struct {
	ConversationID string
	UserID         string
	Query          string
	Config         *MemoryConfig
}

// RecallResult 回忆结果
type RecallResult struct {
	ShortTermMessages []provider.Message
	MidTermSummary    string            // 中期记忆摘要
	GatewaySummary    string            // Gateway 压缩摘要
	KeyFacts          map[string]string // 关键事实
	// LongTermFacts     []Fact   // 预留
}

// Recall 在 LLM 调用前加载记忆
func (c *Coordinator) Recall(ctx context.Context, req *RecallRequest) (*RecallResult, error) {
	result := &RecallResult{}

	applog.Info("[Memory/Coordinator] 🔄 Recall started",
		"conversation_id", req.ConversationID,
		"stm_enabled", req.Config != nil && req.Config.IsShortTermEnabled(),
		"mtm_enabled", req.Config != nil && req.Config.IsMidTermEnabled(),
		"gateway_enabled", req.Config != nil && req.Config.IsGatewayEnabled(),
	)

	if req.Config == nil || req.ConversationID == "" {
		applog.Debug("[Memory/Coordinator] Recall skipped: no config or conversation_id")
		return result, nil
	}

	// 1. 并行加载 STM 和 MTM
	var (
		stmState     *STMState
		stmMessages  []provider.Message
		gatewaySum   string
		keyFacts     map[string]string
		midTermSum   string
		recallLoadWg sync.WaitGroup
	)

	if req.Config.IsShortTermEnabled() {
		redisSTM, ok := c.shortTerm.(*RedisSTM)
		if ok {
			recallLoadWg.Add(1)
			go func() {
				defer recallLoadWg.Done()
				applog.Debug("[Memory/Coordinator] Loading STMState from Redis Hash...", "conversation_id", req.ConversationID)
				state, err := redisSTM.LoadState(ctx, req.ConversationID)
				if err != nil {
					applog.Warn("[Memory/Coordinator] ⚠️ Failed to load STMState",
						"conversation_id", req.ConversationID,
						"error", err,
					)
					return
				}

				stmState = state
				stmMessages = state.RecentMessages
				gatewaySum = state.GatewaySummary
				keyFacts = state.KeyFacts

				applog.Info("[Memory/Coordinator] 💬 STM state loaded",
					"conversation_id", req.ConversationID,
					"messages_count", len(state.RecentMessages),
					"has_gateway_summary", state.GatewaySummary != "",
					"key_facts_count", len(state.KeyFacts),
					"version", state.Version,
				)
			}()
		} else {
			applog.Warn("[Memory/Coordinator] ⚠️ Short-term memory is not RedisSTM, fallback to generic Load",
				"conversation_id", req.ConversationID,
				"type", reflect.TypeOf(c.shortTerm),
			)
			recallLoadWg.Add(1)
			go func() {
				defer recallLoadWg.Done()
				msgs, err := c.shortTerm.Load(ctx, req.ConversationID, req.Config.GetWindowSize())
				if err != nil {
					applog.Warn("[Memory/Coordinator] ⚠️ Failed to load short-term memory",
						"conversation_id", req.ConversationID,
						"error", err,
					)
					return
				}
				stmMessages = msgs
				applog.Info("[Memory/Coordinator] 💬 Short-term memory loaded",
					"conversation_id", req.ConversationID,
					"messages_count", len(msgs),
				)
			}()
		}
	}

	if req.Config.IsMidTermEnabled() && c.midTerm != nil {
		recallLoadWg.Add(1)
		go func() {
			defer recallLoadWg.Done()
			applog.Debug("[Memory/Coordinator] Loading mid-term memory...", "conversation_id", req.ConversationID)
			summary, err := c.midTerm.LoadSummary(ctx, req.ConversationID)
			if err != nil {
				applog.Warn("[Memory/Coordinator] ⚠️ Failed to load mid-term memory",
					"conversation_id", req.ConversationID,
					"error", err,
				)
				return
			}
			if summary != nil && summary.Content != "" {
				midTermSum = summary.Content
				applog.Info("[Memory/Coordinator] 📋 Mid-term summary loaded",
					"conversation_id", req.ConversationID,
					"turns_covered", summary.TurnsCovered,
				)
			}
		}()
	}

	recallLoadWg.Wait()

	result.ShortTermMessages = stmMessages
	result.GatewaySummary = gatewaySum
	result.KeyFacts = keyFacts
	result.MidTermSummary = midTermSum

	_ = stmState // 后续可用于 Token 估算日志

	applog.Info("[Memory/Coordinator] ✅ Recall completed",
		"conversation_id", req.ConversationID,
		"stm_messages", len(result.ShortTermMessages),
		"has_mtm_summary", result.MidTermSummary != "",
		"has_gateway_summary", result.GatewaySummary != "",
		"key_facts_count", len(result.KeyFacts),
	)

	return result, nil
}

// MemorizeRequest 记忆写入请求
type MemorizeRequest struct {
	ConversationID string
	UserID         string
	UserMessage    provider.Message
	AssistantMsg   provider.Message
	Config         *MemoryConfig
}

// Memorize 在 LLM 调用后写入记忆
func (c *Coordinator) Memorize(ctx context.Context, req *MemorizeRequest) error {
	if req.Config == nil || req.ConversationID == "" {
		return nil
	}

	userPreview := req.UserMessage.Content
	if len(userPreview) > 100 {
		userPreview = userPreview[:100] + "..."
	}
	assistantPreview := req.AssistantMsg.Content
	if len(assistantPreview) > 100 {
		assistantPreview = assistantPreview[:100] + "..."
	}

	applog.Info("[Memory/Coordinator] 🔄 Memorize started",
		"conversation_id", req.ConversationID,
		"user_preview", userPreview,
		"assistant_preview", assistantPreview,
	)

	// 1. 追加到短期记忆
	if req.Config.IsShortTermEnabled() {
		err := c.shortTerm.Append(ctx, req.ConversationID, req.UserMessage, req.AssistantMsg)
		if err != nil {
			applog.Warn("[Memory/Coordinator] ⚠️ Failed to append short-term memory",
				"conversation_id", req.ConversationID,
				"error", err,
			)
			return nil
		}
		applog.Info("[Memory/Coordinator] ✅ Short-term memory updated", "conversation_id", req.ConversationID)
	}

	// 2. 检查是否需要触发 Gateway 压缩（Token 阈值）
	if req.Config.IsGatewayEnabled() && c.gateway != nil {
		if redisSTM, ok := c.shortTerm.(*RedisSTM); ok {
			state, err := redisSTM.LoadState(ctx, req.ConversationID)
			if err != nil {
				applog.Warn("[Memory/Coordinator] ⚠️ Failed to load state for compression check",
					"conversation_id", req.ConversationID,
					"error", err,
				)
			} else {
				gwConfig := req.Config.GetGatewayConfig()
				estimator := c.gateway.GetEstimator()
				state.TokenEstimate = EstimateSTMContext(estimator, state)

				if ShouldCompress(estimator, state, gwConfig) {
					applog.Info("[Memory/Coordinator] 🚀 Token threshold exceeded, triggering async compression",
						"conversation_id", req.ConversationID,
						"token_estimate", state.TokenEstimate,
						"threshold", int(float64(gwConfig.GetContextWindowSize())*gwConfig.GetTokenThresholdRatio()),
					)
					go c.compressAsync(req.ConversationID, gwConfig)
				} else {
					applog.Debug("[Memory/Coordinator] Token within budget, no compression needed",
						"conversation_id", req.ConversationID,
						"token_estimate", state.TokenEstimate,
					)
				}
			}
		}
	}

	// 3. 检查是否需要触发中期记忆摘要
	if req.Config.IsMidTermEnabled() && c.midTerm != nil && c.summaryGen != nil {
		applog.Debug("[Memory/Coordinator] Checking mid-term summary trigger...", "conversation_id", req.ConversationID)
		c.maybeGenerateSummary(ctx, req)
	}

	applog.Info("[Memory/Coordinator] ✅ Memorize completed", "conversation_id", req.ConversationID)
	return nil
}

// maybeGenerateSummary 检查轮次阈值，异步触发摘要生成
func (c *Coordinator) maybeGenerateSummary(ctx context.Context, req *MemorizeRequest) {
	threshold := req.Config.MidTerm.GetSummaryThreshold()

	turnCount, err := c.shortTerm.GetTurnCount(ctx, req.ConversationID)
	if err != nil {
		applog.Warn("[Memory/Coordinator] ⚠️ Failed to get turn count",
			"conversation_id", req.ConversationID,
			"error", err,
		)
		return
	}

	applog.Info("[Memory/Coordinator] 📊 Summary check",
		"conversation_id", req.ConversationID,
		"turn_count", turnCount,
		"threshold", threshold,
		"should_trigger", turnCount >= threshold,
	)

	if turnCount < threshold {
		applog.Debug("[Memory/Coordinator] Summary not needed yet",
			"conversation_id", req.ConversationID,
			"turns_remaining", threshold-turnCount,
		)
		return
	}

	// 检查是否已有摘要且已覆盖了一定轮次（避免频繁摘要）
	existingSummary, err := c.midTerm.LoadSummary(ctx, req.ConversationID)
	if err != nil {
		applog.Warn("[Memory/Coordinator] ⚠️ Failed to load existing summary",
			"conversation_id", req.ConversationID,
			"error", err,
		)
		return
	}

	// 如果已有摘要且覆盖轮次 >= turnCount - threshold/2，说明刚摘要过，跳过
	if existingSummary != nil && existingSummary.TurnsCovered >= turnCount-threshold/2 {
		applog.Debug("[Memory/Coordinator] Skipping summary - recently generated",
			"conversation_id", req.ConversationID,
			"existing_turns_covered", existingSummary.TurnsCovered,
			"current_turn_count", turnCount,
		)
		return
	}

	applog.Info("[Memory/Coordinator] 🚀 Triggering async summary generation",
		"conversation_id", req.ConversationID,
		"turn_count", turnCount,
		"existing_summary", existingSummary != nil,
	)

	// 异步生成摘要（继承 scope 到后台上下文）
	go c.generateSummaryAsync(ctx, req.ConversationID, existingSummary)
}

// generateSummaryAsync 异步生成摘要（goroutine 中执行）
func (c *Coordinator) generateSummaryAsync(parentCtx context.Context, conversationID string, existingSummary *ConversationSummary) {
	ctx := context.Background()
	if orgID, tenantID, ok := port.RepoScopeFrom(parentCtx); ok {
		ctx = port.WithRepoScope(ctx, orgID, tenantID)
	}

	applog.Info("[Memory/Coordinator] 🏗️ Async summary generation started", "conversation_id", conversationID)

	// 1. 加载全部短期消息用于摘要
	allMessages, err := c.shortTerm.Load(ctx, conversationID, 0) // 0 = 全部
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Failed to load messages for summary",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	if len(allMessages) == 0 {
		applog.Warn("[Memory/Coordinator] No messages to summarize", "conversation_id", conversationID)
		return
	}

	applog.Info("[Memory/Coordinator] Loaded messages for summary",
		"conversation_id", conversationID,
		"message_count", len(allMessages),
	)

	// 2. 转换为摘要用的 Message 格式
	summaryMsgs := make([]Message, len(allMessages))
	for i, m := range allMessages {
		summaryMsgs[i] = Message{Role: m.Role, Content: m.Content}
	}

	// 3. 生成摘要
	var existing string
	if existingSummary != nil {
		existing = existingSummary.Content
		applog.Debug("[Memory/Coordinator] Including existing summary in generation",
			"conversation_id", conversationID,
			"existing_length", len(existing),
		)
	}

	applog.Info("[Memory/Coordinator] 🤖 Calling LLM for summary generation...",
		"conversation_id", conversationID,
		"message_count", len(summaryMsgs),
		"has_existing", existing != "",
	)

	summaryText, err := c.summaryGen.Summarize(ctx, summaryMsgs, existing)
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Summary generation failed",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	summaryPreview := summaryText
	if len(summaryPreview) > 300 {
		summaryPreview = summaryPreview[:300] + "..."
	}
	applog.Info("[Memory/Coordinator] 📝 Summary generated",
		"conversation_id", conversationID,
		"summary_length", len(summaryText),
		"summary_preview", summaryPreview,
	)

	// 4. 保存摘要
	newSummary := &ConversationSummary{
		Content:      summaryText,
		TurnsCovered: len(allMessages) / 2,
	}

	if err := c.midTerm.SaveSummary(ctx, conversationID, newSummary); err != nil {
		applog.Error("[Memory/Coordinator] ❌ Failed to save summary",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	applog.Info("[Memory/Coordinator] ✅ Summary saved successfully",
		"conversation_id", conversationID,
		"turns_covered", newSummary.TurnsCovered,
		"summary_length", len(summaryText),
	)
}

// compressAsync 异步执行 Context-Gateway 压缩（在 goroutine 中调用）
func (c *Coordinator) compressAsync(conversationID string, gwConfig *GatewayConfig) {
	ctx := context.Background()

	applog.Info("[Memory/Coordinator] 🏗️ Async compression started", "conversation_id", conversationID)

	// 1. 获取分布式锁
	if c.compressLock != nil {
		acquired, err := c.compressLock.Acquire(ctx, conversationID)
		if err != nil {
			applog.Warn("[Memory/Coordinator] ⚠️ Failed to acquire compress lock",
				"conversation_id", conversationID,
				"error", err,
			)
			return
		}
		if !acquired {
			applog.Debug("[Memory/Coordinator] Compress skipped: lock not acquired",
				"conversation_id", conversationID,
			)
			return
		}
		defer c.compressLock.Release(ctx, conversationID)
	}

	// 2. 加载 STMState
	redisSTM, ok := c.shortTerm.(*RedisSTM)
	if !ok {
		applog.Warn("[Memory/Coordinator] Compress skipped: not using RedisSTM", "conversation_id", conversationID)
		return
	}

	state, err := redisSTM.LoadState(ctx, conversationID)
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Failed to load state for compression",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	// 3. 分割消息：选择要压缩的旧消息 + 保留的近期消息
	minRecent := gwConfig.GetMinRecentTurns()
	toCompress, toKeep := SelectMessagesForCompression(state.RecentMessages, minRecent)
	if len(toCompress) == 0 {
		applog.Debug("[Memory/Coordinator] Compress skipped: not enough messages to compress",
			"conversation_id", conversationID,
			"total_messages", len(state.RecentMessages),
			"min_recent_turns", minRecent,
		)
		return
	}

	applog.Info("[Memory/Coordinator] 📦 Compressing messages",
		"conversation_id", conversationID,
		"to_compress", len(toCompress),
		"to_keep", len(toKeep),
	)

	// 4. 调用 Gateway 压缩器
	extractFacts := gwConfig.ExtractKeyFacts
	result, err := c.gateway.Compress(ctx, state.GatewaySummary, toCompress, state.KeyFacts, extractFacts)
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Gateway compression failed",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	// 5. 重新加载 STMState 检查版本（乐观锁）
	currentState, err := redisSTM.LoadState(ctx, conversationID)
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Failed to reload state for version check",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}

	if currentState.Version != state.Version {
		applog.Warn("[Memory/Coordinator] ⚠️ Version mismatch, discarding compression result",
			"conversation_id", conversationID,
			"expected_version", state.Version,
			"current_version", currentState.Version,
		)
		return
	}

	// 6. 更新 STMState
	estimator := c.gateway.GetEstimator()
	expectedVersion := currentState.Version
	currentState.GatewaySummary = result.CompressedSummary
	currentState.KeyFacts = result.KeyFacts
	currentState.RecentMessages = toKeep
	currentState.LastCompressedAt = time.Now().Unix()
	currentState.CompressedTurnCount += len(toCompress) / 2
	currentState.Version = expectedVersion + 1
	currentState.TokenEstimate = EstimateSTMContext(estimator, currentState)

	// 7. CAS 写回 Redis（防止检查后写入窗口内被并发更新覆盖）
	saved, err := redisSTM.SaveStateIfVersion(ctx, conversationID, currentState, expectedVersion)
	if err != nil {
		applog.Error("[Memory/Coordinator] ❌ Failed to save compressed state",
			"conversation_id", conversationID,
			"error", err,
		)
		return
	}
	if !saved {
		applog.Warn("[Memory/Coordinator] ⚠️ Save skipped due to version conflict",
			"conversation_id", conversationID,
			"expected_version", expectedVersion,
		)
		return
	}

	applog.Info("[Memory/Coordinator] ✅ Compression completed successfully",
		"conversation_id", conversationID,
		"compressed_messages", len(toCompress),
		"remaining_messages", len(toKeep),
		"summary_length", len(result.CompressedSummary),
		"key_facts_count", len(result.KeyFacts),
		"new_token_estimate", currentState.TokenEstimate,
		"new_version", currentState.Version,
	)
}
