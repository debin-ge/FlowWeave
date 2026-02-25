package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"flowweave/internal/domain/workflow/port"

	"github.com/redis/go-redis/v9"
)

// PgMTM PostgreSQL + Redis 缓存实现的中期记忆
type PgMTM struct {
	db       *sql.DB
	rds      *redis.Client // 可为 nil（无缓存模式）
	cacheTTL time.Duration
}

// PgMTMConfig PgMTM 配置
type PgMTMConfig struct {
	DB       *sql.DB
	Redis    *redis.Client // 可选，nil 则不缓存
	CacheTTL time.Duration // Redis 缓存 TTL，默认 30 分钟
}

// NewPgMTM 创建 PostgreSQL + Redis 缓存的中期记忆
func NewPgMTM(cfg PgMTMConfig) *PgMTM {
	ttl := cfg.CacheTTL
	if ttl == 0 {
		ttl = 30 * time.Minute
	}
	applog.Info("[MTM/PG] Initialized",
		"has_redis_cache", cfg.Redis != nil,
		"cache_ttl", ttl,
	)
	return &PgMTM{
		db:       cfg.DB,
		rds:      cfg.Redis,
		cacheTTL: ttl,
	}
}

// EnsureTable 确保 conversation_summaries 表存在
func (m *PgMTM) EnsureTable(ctx context.Context) error {
	applog.Info("[MTM/PG] Ensuring conversation_summaries table exists...")
	ddl := `
	CREATE TABLE IF NOT EXISTS conversation_summaries (
		id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		org_id          UUID,
		tenant_id       UUID,
		conversation_id VARCHAR(255) NOT NULL UNIQUE,
		summary         TEXT NOT NULL DEFAULT '',
		turns_covered   INTEGER NOT NULL DEFAULT 0,
		created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_conv_summaries_conv_id ON conversation_summaries(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_summaries_scope_conv ON conversation_summaries(org_id, tenant_id, conversation_id);
	`
	_, err := m.db.ExecContext(ctx, ddl)
	if err != nil {
		applog.Error("[MTM/PG] ❌ Failed to create table", "error", err)
	} else {
		applog.Info("[MTM/PG] ✅ Table ready")
	}
	return err
}

func (m *PgMTM) cacheKey(conversationID, orgID, tenantID string) string {
	if orgID == "" && tenantID == "" {
		return fmt.Sprintf("mtm:v2:%s", conversationID)
	}
	return fmt.Sprintf("mtm:v2:%s:%s:%s", orgID, tenantID, conversationID)
}

// LoadSummary 先查 Redis 缓存，miss 则查 PG 并回填缓存
func (m *PgMTM) LoadSummary(ctx context.Context, conversationID string) (*ConversationSummary, error) {
	applog.Debug("[MTM/PG] Loading summary", "conversation_id", conversationID)
	orgID, tenantID, hasScope := port.RepoScopeFrom(ctx)

	// 1. 尝试 Redis 缓存
	if m.rds != nil {
		cacheKey := m.cacheKey(conversationID, orgID, tenantID)
		cached, err := m.rds.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var summary ConversationSummary
			if json.Unmarshal([]byte(cached), &summary) == nil {
				summaryPreview := summary.Content
				if len(summaryPreview) > 150 {
					summaryPreview = summaryPreview[:150] + "..."
				}
				applog.Info("[MTM/PG] 🎯 Cache HIT",
					"conversation_id", conversationID,
					"cache_key", cacheKey,
					"turns_covered", summary.TurnsCovered,
					"summary_preview", summaryPreview,
				)
				return &summary, nil
			}
			applog.Warn("[MTM/PG] Cache data corrupted, falling through to PG",
				"conversation_id", conversationID,
			)
		} else {
			applog.Debug("[MTM/PG] Cache MISS", "conversation_id", conversationID, "cache_key", cacheKey)
		}
	}

	// 2. 查 PostgreSQL
	applog.Debug("[MTM/PG] Querying PostgreSQL...", "conversation_id", conversationID)
	var summary ConversationSummary
	var err error
	if hasScope {
		err = m.db.QueryRowContext(ctx,
			`SELECT summary, turns_covered, updated_at
			 FROM conversation_summaries
			 WHERE conversation_id = $1 AND org_id = $2 AND tenant_id = $3`,
			conversationID, orgID, tenantID,
		).Scan(&summary.Content, &summary.TurnsCovered, &summary.UpdatedAt)
	} else {
		err = m.db.QueryRowContext(ctx,
			`SELECT summary, turns_covered, updated_at FROM conversation_summaries WHERE conversation_id = $1`,
			conversationID,
		).Scan(&summary.Content, &summary.TurnsCovered, &summary.UpdatedAt)
	}

	if err == sql.ErrNoRows {
		applog.Debug("[MTM/PG] No summary found in PG", "conversation_id", conversationID)
		return nil, nil
	}
	if err != nil {
		applog.Error("[MTM/PG] ❌ PG query failed", "conversation_id", conversationID, "error", err)
		return nil, fmt.Errorf("pg load summary: %w", err)
	}

	summaryPreview := summary.Content
	if len(summaryPreview) > 150 {
		summaryPreview = summaryPreview[:150] + "..."
	}
	applog.Info("[MTM/PG] 📥 Loaded from PG",
		"conversation_id", conversationID,
		"turns_covered", summary.TurnsCovered,
		"updated_at", summary.UpdatedAt,
		"summary_preview", summaryPreview,
	)

	// 3. 回填 Redis 缓存
	if m.rds != nil {
		m.setCache(ctx, conversationID, orgID, tenantID, &summary)
	}

	return &summary, nil
}

// SaveSummary 写 PG + 失效 Redis 缓存
func (m *PgMTM) SaveSummary(ctx context.Context, conversationID string, summary *ConversationSummary) error {
	summary.UpdatedAt = time.Now()
	orgID, tenantID, hasScope := port.RepoScopeFrom(ctx)

	summaryPreview := summary.Content
	if len(summaryPreview) > 150 {
		summaryPreview = summaryPreview[:150] + "..."
	}
	applog.Info("[MTM/PG] 💾 Saving summary to PG",
		"conversation_id", conversationID,
		"turns_covered", summary.TurnsCovered,
		"summary_length", len(summary.Content),
		"summary_preview", summaryPreview,
	)

	var err error
	if hasScope {
		_, err = m.db.ExecContext(ctx,
			`INSERT INTO conversation_summaries (conversation_id, org_id, tenant_id, summary, turns_covered, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (conversation_id) DO UPDATE
			 SET org_id = EXCLUDED.org_id,
			     tenant_id = EXCLUDED.tenant_id,
			     summary = EXCLUDED.summary,
			     turns_covered = EXCLUDED.turns_covered,
			     updated_at = EXCLUDED.updated_at`,
			conversationID, orgID, tenantID, summary.Content, summary.TurnsCovered, summary.UpdatedAt,
		)
	} else {
		_, err = m.db.ExecContext(ctx,
			`INSERT INTO conversation_summaries (conversation_id, summary, turns_covered, updated_at)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (conversation_id) DO UPDATE
			 SET summary = EXCLUDED.summary,
			     turns_covered = EXCLUDED.turns_covered,
			     updated_at = EXCLUDED.updated_at`,
			conversationID, summary.Content, summary.TurnsCovered, summary.UpdatedAt,
		)
	}
	if err != nil {
		applog.Error("[MTM/PG] ❌ PG save failed", "conversation_id", conversationID, "error", err)
		return fmt.Errorf("pg save summary: %w", err)
	}

	applog.Info("[MTM/PG] ✅ Summary saved to PG", "conversation_id", conversationID)

	// 失效 Redis 缓存（下次读时回填最新数据）
	if m.rds != nil {
		cacheKey := m.cacheKey(conversationID, orgID, tenantID)
		if delErr := m.rds.Del(ctx, cacheKey).Err(); delErr != nil {
			applog.Warn("[MTM/PG] ⚠️ Failed to invalidate cache",
				"conversation_id", conversationID,
				"cache_key", cacheKey,
				"error", delErr,
			)
		} else {
			applog.Debug("[MTM/PG] Cache invalidated", "conversation_id", conversationID, "cache_key", cacheKey)
		}
	}

	return nil
}

// setCache 写 Redis 缓存
func (m *PgMTM) setCache(ctx context.Context, conversationID, orgID, tenantID string, summary *ConversationSummary) {
	cacheKey := m.cacheKey(conversationID, orgID, tenantID)
	data, err := json.Marshal(summary)
	if err != nil {
		applog.Warn("[MTM/PG] Failed to marshal summary for cache", "conversation_id", conversationID, "error", err)
		return
	}
	if setErr := m.rds.Set(ctx, cacheKey, data, m.cacheTTL).Err(); setErr != nil {
		applog.Warn("[MTM/PG] ⚠️ Failed to set cache",
			"conversation_id", conversationID,
			"cache_key", cacheKey,
			"error", setErr,
		)
	} else {
		applog.Debug("[MTM/PG] Cache populated",
			"conversation_id", conversationID,
			"cache_key", cacheKey,
			"ttl", m.cacheTTL,
		)
	}
}
