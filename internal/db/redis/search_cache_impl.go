package redisdb

import (
	applog "flowweave/internal/platform/log"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	domainrag "flowweave/internal/domain/rag"

	"github.com/redis/go-redis/v9"
)

// SearchCache 检索结果 Redis 缓存
type SearchCache struct {
	redis  *redis.Client
	ttl    time.Duration
	prefix string
}

// NewSearchCache 创建检索缓存
func NewSearchCache(rdb *redis.Client, ttlSeconds int) *SearchCache {
	ttl := 5 * time.Minute
	if ttlSeconds > 0 {
		ttl = time.Duration(ttlSeconds) * time.Second
	}
	return &SearchCache{
		redis:  rdb,
		ttl:    ttl,
		prefix: "rag:cache:",
	}
}

// Get 从缓存获取检索结果
func (c *SearchCache) Get(ctx context.Context, req *domainrag.SearchRequest) (*domainrag.SearchResult, bool) {
	key := c.cacheKey(req)
	data, err := c.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}

	var result domainrag.SearchResult
	if err := json.Unmarshal(data, &result); err != nil {
		applog.Warn("[RAG/Cache] Failed to unmarshal cached result", "error", err)
		return nil, false
	}

	applog.Debug("[RAG/Cache] Hit", "key", key)
	return &result, true
}

// Set 写入检索结果到缓存
func (c *SearchCache) Set(ctx context.Context, req *domainrag.SearchRequest, result *domainrag.SearchResult) {
	key := c.cacheKey(req)
	data, err := json.Marshal(result)
	if err != nil {
		return
	}

	if err := c.redis.Set(ctx, key, data, c.ttl).Err(); err != nil {
		applog.Warn("[RAG/Cache] Failed to set cache", "key", key, "error", err)
	}
}

// InvalidateByDataset 按 datasetID 清除相关缓存（模式匹配删除）
func (c *SearchCache) InvalidateByDataset(ctx context.Context, datasetID string) {
	// 使用 SCAN 找到包含 dataset 关键字的缓存并删除
	pattern := c.prefix + "*"
	iter := c.redis.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if len(keys) > 0 {
		c.redis.Del(ctx, keys...)
		applog.Info("[RAG/Cache] Invalidated", "dataset_id", datasetID, "keys_deleted", len(keys))
	}
}

// InvalidateAll 清除所有 RAG 缓存
func (c *SearchCache) InvalidateAll(ctx context.Context) {
	pattern := c.prefix + "*"
	iter := c.redis.Scan(ctx, 0, pattern, 500).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if len(keys) > 0 {
		c.redis.Del(ctx, keys...)
		applog.Info("[RAG/Cache] All cache invalidated", "keys_deleted", len(keys))
	}
}

// cacheKey 生成缓存 key = hash(query + mode + topk + datasetIDs + tenantID)
func (c *SearchCache) cacheKey(req *domainrag.SearchRequest) string {
	// 排序 datasetIDs 确保一致性
	ids := make([]string, len(req.DatasetIDs))
	copy(ids, req.DatasetIDs)
	sort.Strings(ids)

	raw := fmt.Sprintf("%s|%s|%d|%s|%s|%s",
		req.Query,
		req.Mode,
		req.TopK,
		strings.Join(ids, ","),
		req.OrgID,
		req.TenantID,
	)

	hash := sha256.Sum256([]byte(raw))
	return c.prefix + fmt.Sprintf("%x", hash[:12])
}
