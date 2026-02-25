package rag

import (
	applog "flowweave/internal/platform/log"
	"os"
	"strconv"
)

// Config RAG 模块配置
type Config struct {
	// OpenSearch 连接
	OpenSearchURL      string `json:"opensearch_url"`
	OpenSearchUsername string `json:"opensearch_username"`
	OpenSearchPassword string `json:"opensearch_password"`
	IndexPrefix        string `json:"index_prefix"`

	// 检索配置
	DefaultMode  RetrievalMode `json:"default_mode"`
	EnableRerank bool          `json:"enable_rerank"`
	DefaultTopK  int           `json:"default_top_k"`

	// Embedding (Phase 2)
	EmbeddingProvider string `json:"embedding_provider,omitempty"`
	EmbeddingModel    string `json:"embedding_model,omitempty"`
	EmbeddingDims     int    `json:"embedding_dims,omitempty"`

	// Rerank (Phase 2)
	RerankProvider string `json:"rerank_provider,omitempty"`
	RerankModel    string `json:"rerank_model,omitempty"`

	// Chunker 配置
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`

	// 缓存配置
	CacheTTL    int `json:"cache_ttl"`     // 缓存 TTL（秒），0=禁用
	MaxFileSize int `json:"max_file_size"` // 最大文件大小（MB）
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		OpenSearchURL: "https://localhost:9200",
		IndexPrefix:   "rag",
		DefaultMode:   RetrievalModeBM25,
		EnableRerank:  false,
		DefaultTopK:   5,
		EmbeddingDims: 768,
		ChunkSize:     512,
		ChunkOverlap:  128,
		CacheTTL:      300, // 5分钟
		MaxFileSize:   50,  // 50MB
	}
}

// LoadConfigFromEnv 从环境变量加载配置
func LoadConfigFromEnv() *Config {
	cfg := DefaultConfig()

	if v := os.Getenv("OPENSEARCH_URL"); v != "" {
		cfg.OpenSearchURL = v
	}
	if v := os.Getenv("OPENSEARCH_USERNAME"); v != "" {
		cfg.OpenSearchUsername = v
	}
	if v := os.Getenv("OPENSEARCH_PASSWORD"); v != "" {
		cfg.OpenSearchPassword = v
	}
	if v := os.Getenv("OPENSEARCH_INDEX_PREFIX"); v != "" {
		cfg.IndexPrefix = v
	}

	if v := os.Getenv("RAG_DEFAULT_MODE"); v != "" {
		cfg.DefaultMode = RetrievalMode(v)
	}
	if v := os.Getenv("RAG_ENABLE_RERANK"); v == "true" {
		cfg.EnableRerank = true
	}
	if v := os.Getenv("RAG_DEFAULT_TOP_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.DefaultTopK = n
		}
	}

	if v := os.Getenv("RAG_EMBEDDING_PROVIDER"); v != "" {
		cfg.EmbeddingProvider = v
	}
	if v := os.Getenv("RAG_EMBEDDING_MODEL"); v != "" {
		cfg.EmbeddingModel = v
	}
	if v := os.Getenv("RAG_EMBEDDING_DIMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.EmbeddingDims = n
		}
	}

	if v := os.Getenv("RAG_RERANK_PROVIDER"); v != "" {
		cfg.RerankProvider = v
	}
	if v := os.Getenv("RAG_RERANK_MODEL"); v != "" {
		cfg.RerankModel = v
	}

	if v := os.Getenv("RAG_CHUNK_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ChunkSize = n
		}
	}
	if v := os.Getenv("RAG_CHUNK_OVERLAP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.ChunkOverlap = n
		}
	}
	if v := os.Getenv("RAG_CACHE_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.CacheTTL = n
		}
	}
	if v := os.Getenv("RAG_MAX_FILE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxFileSize = n
		}
	}

	applog.Info("[RAG] Config loaded",
		"opensearch_url", cfg.OpenSearchURL,
		"index_prefix", cfg.IndexPrefix,
		"default_mode", cfg.DefaultMode,
		"default_top_k", cfg.DefaultTopK,
		"cache_ttl", cfg.CacheTTL,
		"max_file_size_mb", cfg.MaxFileSize,
	)

	return cfg
}

// ChunkIndexName 返回 Chunk 索引名称
func (c *Config) ChunkIndexName() string {
	return c.IndexPrefix + "_chunk_index"
}

// HasEmbedding 是否配置了 Embedding
func (c *Config) HasEmbedding() bool {
	return c.EmbeddingProvider != "" && c.EmbeddingModel != ""
}

// HasRerank 是否配置了 Rerank
func (c *Config) HasRerank() bool {
	return c.EnableRerank && c.RerankProvider != "" && c.RerankModel != ""
}

// HasCache 是否启用缓存
func (c *Config) HasCache() bool {
	return c.CacheTTL > 0
}
