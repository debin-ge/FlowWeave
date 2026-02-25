package rag

import "time"

// ChunkDocument 文档分块后的数据结构
type ChunkDocument struct {
	DocID     string            `json:"doc_id"`
	ChunkID   string            `json:"chunk_id"`
	DatasetID string            `json:"dataset_id"`
	OrgID     string            `json:"org_id"`
	TenantID  string            `json:"tenant_id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Vector    []float32         `json:"vector,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Source    string            `json:"source,omitempty"`
	Page      int               `json:"page,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// RetrievalMode 检索模式
type RetrievalMode string

const (
	RetrievalModeBM25   RetrievalMode = "BM25"
	RetrievalModeHybrid RetrievalMode = "HYBRID"
	RetrievalModeAuto   RetrievalMode = "AUTO"
)

// SearchRequest 检索请求
type SearchRequest struct {
	Query          string            `json:"query"`
	Mode           RetrievalMode     `json:"mode,omitempty"`
	TopK           int               `json:"top_k,omitempty"`
	ScoreThreshold float64           `json:"score_threshold,omitempty"`
	DatasetIDs     []string          `json:"dataset_ids,omitempty"`
	Filters        map[string]string `json:"filters,omitempty"`
	// 多租户（从 Scope 自动注入）
	OrgID    string `json:"-"`
	TenantID string `json:"-"`
}

// SearchResult 检索结果
type SearchResult struct {
	Documents []ResultDocument `json:"documents"`
	Mode      RetrievalMode    `json:"mode"`
	ElapsedMs int64            `json:"elapsed_ms"`
}

// ResultDocument 单条检索结果
type ResultDocument struct {
	ChunkID   string  `json:"chunk_id"`
	Content   string  `json:"content"`
	Score     float64 `json:"score"`
	Source    string  `json:"source,omitempty"`
	Page      int     `json:"page,omitempty"`
	Title     string  `json:"title,omitempty"`
	DatasetID string  `json:"dataset_id,omitempty"`
}

// QAPair 问答对
type QAPair struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// IndexRequest 文档入库请求
type IndexRequest struct {
	DatasetID string   `json:"dataset_id"`
	OrgID     string   `json:"org_id"`
	TenantID  string   `json:"tenant_id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Source    string   `json:"source,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	QAPairs   []QAPair `json:"qa_pairs,omitempty"` // QA 对（每对独立成 chunk）
	Format    string   `json:"format,omitempty"`   // 文件格式提示
}

// IndexResult 入库结果
type IndexResult struct {
	DocID      string `json:"doc_id"`
	ChunkCount int    `json:"chunk_count"`
}
