package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"flowweave/internal/provider"
)

// RedisSTM Redis Hash 实现的短期记忆
type RedisSTM struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// RedisSTMConfig Redis 短期记忆配置
type RedisSTMConfig struct {
	Client    *redis.Client
	KeyPrefix string        // 默认 "stm:"
	TTL       time.Duration // 默认 24h
}

// NewRedisSTM 创建 Redis 短期记忆
func NewRedisSTM(cfg RedisSTMConfig) *RedisSTM {
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "stm:v2:"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 24 * time.Hour
	}
	return &RedisSTM{
		client:    cfg.Client,
		keyPrefix: cfg.KeyPrefix,
		ttl:       cfg.TTL,
	}
}

func (s *RedisSTM) key(conversationID string) string {
	return s.keyPrefix + conversationID
}

// LoadState 加载完整的 STM 状态（从 Redis Hash）
func (s *RedisSTM) LoadState(ctx context.Context, conversationID string) (*STMState, error) {
	key := s.key(conversationID)

	applog.Debug("[STM/Redis] Loading state", "conversation_id", conversationID, "key", key)

	vals, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		applog.Error("[STM/Redis] HGETALL failed", "conversation_id", conversationID, "error", err)
		return nil, fmt.Errorf("redis HGETALL: %w", err)
	}

	state := &STMState{
		SessionID: conversationID,
		KeyFacts:  make(map[string]string),
	}

	if len(vals) == 0 {
		applog.Debug("[STM/Redis] No state found, returning empty", "conversation_id", conversationID)
		return state, nil
	}

	// 解析 recent_messages
	if raw, ok := vals["recent_messages"]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &state.RecentMessages); err != nil {
			applog.Warn("[STM/Redis] Failed to parse recent_messages", "conversation_id", conversationID, "error", err)
		}
	}

	// 解析 gateway_summary
	if raw, ok := vals["gateway_summary"]; ok {
		state.GatewaySummary = raw
	}

	// 解析 key_facts
	if raw, ok := vals["key_facts"]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &state.KeyFacts); err != nil {
			applog.Warn("[STM/Redis] Failed to parse key_facts", "conversation_id", conversationID, "error", err)
			state.KeyFacts = make(map[string]string)
		}
	}

	// 解析数值字段
	if raw, ok := vals["token_estimate"]; ok {
		state.TokenEstimate, _ = strconv.Atoi(raw)
	}
	if raw, ok := vals["last_compressed_at"]; ok {
		state.LastCompressedAt, _ = strconv.ParseInt(raw, 10, 64)
	}
	if raw, ok := vals["compressed_turn_count"]; ok {
		state.CompressedTurnCount, _ = strconv.Atoi(raw)
	}
	if raw, ok := vals["version"]; ok {
		state.Version, _ = strconv.Atoi(raw)
	}

	applog.Info("[STM/Redis] State loaded",
		"conversation_id", conversationID,
		"recent_messages", len(state.RecentMessages),
		"has_summary", state.GatewaySummary != "",
		"key_facts_count", len(state.KeyFacts),
		"token_estimate", state.TokenEstimate,
		"version", state.Version,
	)

	return state, nil
}

// SaveState 保存完整的 STM 状态（到 Redis Hash）
func (s *RedisSTM) SaveState(ctx context.Context, conversationID string, state *STMState) error {
	key := s.key(conversationID)

	applog.Debug("[STM/Redis] Saving state", "conversation_id", conversationID, "key", key)

	fields := buildStateFields(state)

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, s.ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		applog.Error("[STM/Redis] Pipeline exec failed", "conversation_id", conversationID, "error", err)
		return fmt.Errorf("redis pipeline: %w", err)
	}

	applog.Info("[STM/Redis] State saved",
		"conversation_id", conversationID,
		"recent_messages", len(state.RecentMessages),
		"version", state.Version,
	)
	return nil
}

// SaveStateIfVersion 仅在当前版本等于 expectedVersion 时保存（CAS）
func (s *RedisSTM) SaveStateIfVersion(ctx context.Context, conversationID string, state *STMState, expectedVersion int) (bool, error) {
	key := s.key(conversationID)
	fields := buildStateFields(state)

	err := s.client.Watch(ctx, func(tx *redis.Tx) error {
		currentVersion, err := tx.HGet(ctx, key, "version").Int()
		if err == redis.Nil {
			currentVersion = 0
		} else if err != nil {
			return err
		}

		if currentVersion != expectedVersion {
			return ErrSTMVersionConflict
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, key, fields)
			pipe.Expire(ctx, key, s.ttl)
			return nil
		})
		return err
	}, key)

	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrSTMVersionConflict), errors.Is(err, redis.TxFailedErr):
		return false, nil
	default:
		return false, fmt.Errorf("redis cas save: %w", err)
	}
}

// --- ShortTermMemory 接口实现（保持向后兼容） ---

func (s *RedisSTM) Load(ctx context.Context, conversationID string, windowSize int) ([]provider.Message, error) {
	state, err := s.LoadState(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	messages := state.RecentMessages
	if len(messages) == 0 {
		return nil, nil
	}

	// 窗口大小按消息条数（每轮 2 条：user + assistant）
	maxMsgs := windowSize * 2
	if maxMsgs > 0 && len(messages) > maxMsgs {
		messages = messages[len(messages)-maxMsgs:]
	}

	applog.Info("[STM/Redis] Messages loaded via Load()",
		"conversation_id", conversationID,
		"total_messages", len(messages),
		"window_size", windowSize,
	)

	return messages, nil
}

func (s *RedisSTM) Append(ctx context.Context, conversationID string, msgs ...provider.Message) error {
	applog.Info("[STM/Redis] Appending messages",
		"conversation_id", conversationID,
		"count", len(msgs),
	)

	const maxRetry = 5
	for attempt := 1; attempt <= maxRetry; attempt++ {
		// 加载当前状态
		state, err := s.LoadState(ctx, conversationID)
		if err != nil {
			return err
		}

		baseVersion := state.Version

		// 追加消息并递增版本
		state.RecentMessages = append(state.RecentMessages, msgs...)
		state.Version = baseVersion + 1

		// CAS 保存，防止并发覆盖
		saved, err := s.SaveStateIfVersion(ctx, conversationID, state, baseVersion)
		if err != nil {
			return err
		}
		if saved {
			return nil
		}

		applog.Warn("[STM/Redis] Append version conflict, retrying",
			"conversation_id", conversationID,
			"attempt", attempt,
			"max_retry", maxRetry,
		)
	}

	return fmt.Errorf("append failed after retries: %w", ErrSTMVersionConflict)
}

func (s *RedisSTM) GetTurnCount(ctx context.Context, conversationID string) (int, error) {
	state, err := s.LoadState(ctx, conversationID)
	if err != nil {
		return 0, err
	}

	turnCount := len(state.RecentMessages) / 2
	applog.Debug("[STM/Redis] Turn count",
		"conversation_id", conversationID,
		"raw_length", len(state.RecentMessages),
		"turn_count", turnCount,
	)
	return turnCount, nil
}

func (s *RedisSTM) Clear(ctx context.Context, conversationID string) error {
	key := s.key(conversationID)
	applog.Info("[STM/Redis] Clearing conversation",
		"conversation_id", conversationID,
		"key", key,
	)
	return s.client.Del(ctx, key).Err()
}

// GetClient 返回 Redis 客户端（供 Coordinator 和其他组件使用）
func (s *RedisSTM) GetClient() *redis.Client {
	return s.client
}

func buildStateFields(state *STMState) map[string]interface{} {
	fields := make(map[string]interface{})

	// 序列化 recent_messages
	if msgData, err := json.Marshal(state.RecentMessages); err == nil {
		fields["recent_messages"] = string(msgData)
	}

	fields["gateway_summary"] = state.GatewaySummary

	// 序列化 key_facts
	if factsData, err := json.Marshal(state.KeyFacts); err == nil {
		fields["key_facts"] = string(factsData)
	}

	fields["token_estimate"] = strconv.Itoa(state.TokenEstimate)
	fields["last_compressed_at"] = strconv.FormatInt(state.LastCompressedAt, 10)
	fields["compressed_turn_count"] = strconv.Itoa(state.CompressedTurnCount)
	fields["version"] = strconv.Itoa(state.Version)

	return fields
}
