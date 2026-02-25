package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCompressLock 基于 Redis SETNX 的分布式压缩锁
type RedisCompressLock struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCompressLock 创建分布式压缩锁
func NewRedisCompressLock(client *redis.Client) *RedisCompressLock {
	return &RedisCompressLock{
		client: client,
		ttl:    30 * time.Second,
	}
}

// Acquire 获取压缩锁
func (l *RedisCompressLock) Acquire(ctx context.Context, conversationID string) (bool, error) {
	key := fmt.Sprintf("stm:v2:lock:%s", conversationID)
	acquired, err := l.client.SetNX(ctx, key, "locked", l.ttl).Result()
	if err != nil {
		applog.Warn("[CompressLock] Failed to acquire lock",
			"conversation_id", conversationID,
			"error", err,
		)
		return false, err
	}

	if acquired {
		applog.Debug("[CompressLock] Lock acquired", "conversation_id", conversationID)
	} else {
		applog.Debug("[CompressLock] Lock already held", "conversation_id", conversationID)
	}

	return acquired, nil
}

// Release 释放压缩锁
func (l *RedisCompressLock) Release(ctx context.Context, conversationID string) error {
	key := fmt.Sprintf("stm:v2:lock:%s", conversationID)
	err := l.client.Del(ctx, key).Err()
	if err != nil {
		applog.Warn("[CompressLock] Failed to release lock",
			"conversation_id", conversationID,
			"error", err,
		)
		return err
	}

	applog.Debug("[CompressLock] Lock released", "conversation_id", conversationID)
	return nil
}
