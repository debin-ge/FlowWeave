package redisdb

import (
	goredis "github.com/redis/go-redis/v9"

	memorypkg "flowweave/internal/domain/memory"
)

// CompressLock aliases the Redis-backed compression lock.
type CompressLock = memorypkg.RedisCompressLock

func NewCompressLock(client *goredis.Client) *CompressLock {
	return memorypkg.NewRedisCompressLock(client)
}
