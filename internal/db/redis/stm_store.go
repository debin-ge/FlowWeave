package redisdb

import memorypkg "flowweave/internal/domain/memory"

// STMStore aliases the Redis-backed STM implementation.
type STMStore = memorypkg.RedisSTM

type STMStoreConfig = memorypkg.RedisSTMConfig

func NewSTMStore(cfg STMStoreConfig) *STMStore {
	return memorypkg.NewRedisSTM(cfg)
}
