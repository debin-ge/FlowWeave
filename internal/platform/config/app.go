package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"flowweave/internal/domain/rag"
)

// AppConfig 全局配置。启动时统一加载，再按模块提取使用。
type AppConfig struct {
	LogLevel  string         `json:"log_level"`
	LogFormat string         `json:"log_format"`
	Server    ServerConfig   `json:"server"`
	Database  DatabaseConfig `json:"database"`
	Redis     RedisConfig    `json:"redis"`
	Engine    EngineConfig   `json:"engine"`
	Auth      AuthConfig     `json:"auth"`
	OpenAI    OpenAIConfig   `json:"openai"`
	Summary   SummaryConfig  `json:"summary"`
	Gateway   GatewayConfig  `json:"gateway"`
	RAG       rag.Config     `json:"rag"`
}

type ServerConfig struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	ReadTimeoutSeconds  int    `json:"read_timeout_seconds"`
	WriteTimeoutSeconds int    `json:"write_timeout_seconds"`
}

type DatabaseConfig struct {
	URL                    string `json:"url"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

type RedisConfig struct {
	URL string `json:"url"`
}

type EngineConfig struct {
	MaxWorkers         int `json:"max_workers"`
	NodeTimeoutSeconds int `json:"node_timeout_seconds"`
	MaxNodeSteps       int `json:"max_node_steps"`
}

type AuthConfig struct {
	JWTSecret string `json:"jwt_secret"`
	JWTIssuer string `json:"jwt_issuer"`
}

type OpenAIConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

type SummaryConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type GatewayConfig struct {
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	ContextWindowSize int     `json:"context_window_size"`
	ThresholdRatio    float64 `json:"threshold_ratio"`
	MinRecentTurns    int     `json:"min_recent_turns"`
}

// Default 返回默认配置。
func Default() *AppConfig {
	ragCfg := rag.DefaultConfig()
	return &AppConfig{
		LogLevel:  "info",
		LogFormat: "text",
		Server: ServerConfig{
			Host:                "0.0.0.0",
			Port:                8080,
			ReadTimeoutSeconds:  30,
			WriteTimeoutSeconds: 600,
		},
		Database: DatabaseConfig{
			MaxOpenConns:           25,
			MaxIdleConns:           5,
			ConnMaxLifetimeSeconds: 300,
		},
		Engine: EngineConfig{
			MaxWorkers:         4,
			NodeTimeoutSeconds: 300,
			MaxNodeSteps:       100,
		},
		OpenAI: OpenAIConfig{
			BaseURL: "https://api.openai.com/v1",
		},
		Summary: SummaryConfig{
			Provider: "openai",
			Model:    "gpt-4o-mini",
		},
		Gateway: GatewayConfig{
			Model:             "gpt-4o-mini",
			ContextWindowSize: 128000,
			ThresholdRatio:    0.70,
			MinRecentTurns:    4,
		},
		RAG: *ragCfg,
	}
}

// Load 加载全局配置：默认值 -> 配置文件 -> 环境变量。
// 配置文件路径通过 APP_CONFIG_FILE 指定（JSON）。
func Load() (*AppConfig, error) {
	if err := godotenv.Load(); err != nil {
		// .env 非必需，忽略错误
	}

	cfg := Default()

	if path := strings.TrimSpace(os.Getenv("APP_CONFIG_FILE")); path != "" {
		if err := cfg.loadFile(path); err != nil {
			return nil, err
		}
	}

	cfg.applyEnv()
	cfg.normalize()

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *AppConfig) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read APP_CONFIG_FILE %q failed: %w", path, err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		return fmt.Errorf("parse APP_CONFIG_FILE %q failed: %w", path, err)
	}
	return nil
}

func (c *AppConfig) applyEnv() {
	applyString("LOG_LEVEL", &c.LogLevel)
	applyString("LOG_FORMAT", &c.LogFormat)

	applyString("HOST", &c.Server.Host)
	applyInt("PORT", &c.Server.Port)
	applyInt("SERVER_READ_TIMEOUT", &c.Server.ReadTimeoutSeconds)
	applyInt("SERVER_WRITE_TIMEOUT", &c.Server.WriteTimeoutSeconds)

	applyString("DATABASE_URL", &c.Database.URL)
	applyInt("DATABASE_MAX_OPEN_CONNS", &c.Database.MaxOpenConns)
	applyInt("DATABASE_MAX_IDLE_CONNS", &c.Database.MaxIdleConns)
	applyInt("DATABASE_CONN_MAX_LIFETIME", &c.Database.ConnMaxLifetimeSeconds)

	applyString("REDIS_URL", &c.Redis.URL)

	applyInt("ENGINE_MAX_WORKERS", &c.Engine.MaxWorkers)
	applyInt("ENGINE_NODE_TIMEOUT", &c.Engine.NodeTimeoutSeconds)
	applyInt("ENGINE_MAX_NODE_STEPS", &c.Engine.MaxNodeSteps)

	applyString("JWT_SECRET", &c.Auth.JWTSecret)
	applyString("JWT_ISSUER", &c.Auth.JWTIssuer)

	applyString("OPENAI_API_KEY", &c.OpenAI.APIKey)
	applyString("OPENAI_BASE_URL", &c.OpenAI.BaseURL)

	applyString("SUMMARY_LLM_PROVIDER", &c.Summary.Provider)
	applyString("SUMMARY_LLM_MODEL", &c.Summary.Model)

	applyString("GATEWAY_LLM_PROVIDER", &c.Gateway.Provider)
	applyString("GATEWAY_LLM_MODEL", &c.Gateway.Model)
	applyInt("CONTEXT_WINDOW_SIZE", &c.Gateway.ContextWindowSize)
	applyFloat64("COMPRESS_THRESHOLD_RATIO", &c.Gateway.ThresholdRatio)
	applyInt("COMPRESS_MIN_RECENT_TURNS", &c.Gateway.MinRecentTurns)

	// RAG 环境变量
	applyString("OPENSEARCH_URL", &c.RAG.OpenSearchURL)
	applyString("OPENSEARCH_USERNAME", &c.RAG.OpenSearchUsername)
	applyString("OPENSEARCH_PASSWORD", &c.RAG.OpenSearchPassword)
	applyString("OPENSEARCH_INDEX_PREFIX", &c.RAG.IndexPrefix)
	if v := os.Getenv("RAG_DEFAULT_MODE"); v != "" {
		c.RAG.DefaultMode = rag.RetrievalMode(v)
	}
	if v := os.Getenv("RAG_ENABLE_RERANK"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.RAG.EnableRerank = b
		}
	}
	applyInt("RAG_DEFAULT_TOP_K", &c.RAG.DefaultTopK)
	applyString("RAG_EMBEDDING_PROVIDER", &c.RAG.EmbeddingProvider)
	applyString("RAG_EMBEDDING_MODEL", &c.RAG.EmbeddingModel)
	applyInt("RAG_EMBEDDING_DIMS", &c.RAG.EmbeddingDims)
	applyString("RAG_RERANK_PROVIDER", &c.RAG.RerankProvider)
	applyString("RAG_RERANK_MODEL", &c.RAG.RerankModel)
	applyInt("RAG_CHUNK_SIZE", &c.RAG.ChunkSize)
	applyInt("RAG_CHUNK_OVERLAP", &c.RAG.ChunkOverlap)
	applyInt("RAG_CACHE_TTL", &c.RAG.CacheTTL)
	applyInt("RAG_MAX_FILE_SIZE", &c.RAG.MaxFileSize)
}

func (c *AppConfig) normalize() {
	if c.OpenAI.BaseURL == "" {
		c.OpenAI.BaseURL = "https://api.openai.com/v1"
	}
	if c.Gateway.Provider == "" {
		c.Gateway.Provider = c.Summary.Provider
	}
	if c.Gateway.Model == "" {
		c.Gateway.Model = c.Summary.Model
	}
}

func (c *AppConfig) validate() error {
	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if strings.TrimSpace(c.Redis.URL) == "" {
		return fmt.Errorf("REDIS_URL is required")
	}
	return nil
}

func applyString(key string, target *string) {
	if v := os.Getenv(key); v != "" {
		*target = v
	}
}

func applyInt(key string, target *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*target = n
		}
	}
}

func applyFloat64(key string, target *float64) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			*target = n
		}
	}
}
