package rag

import (
	applog "flowweave/internal/platform/log"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── Embedder 接口 ──────────────────────────────────────────────

// Embedder 向量生成接口
type Embedder interface {
	// Embed 将文本列表转为向量（batch）
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dims 返回向量维度
	Dims() int
}

// ── OpenAI 兼容 Embedder 实现 ─────────────────────────────────

// OpenAIEmbedder 调用 OpenAI 兼容 /v1/embeddings API
type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	dims    int
	client  *http.Client
}

// OpenAIEmbedderConfig 配置
type OpenAIEmbedderConfig struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Model   string // e.g. text-embedding-3-small
	Dims    int    // 向量维度
}

// NewOpenAIEmbedder 创建 OpenAI 兼容 Embedder
func NewOpenAIEmbedder(cfg OpenAIEmbedderConfig) *OpenAIEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	if cfg.Dims <= 0 {
		cfg.Dims = 1536
	}

	return &OpenAIEmbedder{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		dims:    cfg.Dims,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Dims 返回向量维度
func (e *OpenAIEmbedder) Dims() int {
	return e.dims
}

// Embed 批量生成向量
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 分批处理（每批最多 64 条，避免 API 限制）
	const batchSize = 64
	allVectors := make([][]float32, 0, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		vectors, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embed batch %d-%d: %w", i, end, err)
		}
		allVectors = append(allVectors, vectors...)
	}

	return allVectors, nil
}

// ── 内部请求/响应结构 ──────────────────────────────────────────

type embeddingRequest struct {
	Input          interface{} `json:"input"`
	Model          string      `json:"model"`
	Dimensions     int         `json:"dimensions,omitempty"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
}

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Model string          `json:"model"`
	Usage embeddingUsage  `json:"usage"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// embedBatch 单批次 Embedding
func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	start := time.Now()

	reqBody := embeddingRequest{
		Input:          texts,
		Model:          e.model,
		EncodingFormat: "float",
	}
	// 若使用支持 dimensions 参数的模型（如 text-embedding-3-*）
	if strings.Contains(e.model, "embedding-3") {
		reqBody.Dimensions = e.dims
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// 按 index 排序确保顺序正确
	vectors := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index >= 0 && d.Index < len(vectors) {
			vectors[d.Index] = d.Embedding
		}
	}

	// 验证所有向量都已填充
	for i, v := range vectors {
		if v == nil {
			return nil, fmt.Errorf("missing embedding for text index %d", i)
		}
	}

	applog.Debug("[RAG/Embedder] Batch embedded",
		"count", len(texts),
		"dims", len(vectors[0]),
		"tokens", embResp.Usage.TotalTokens,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)

	return vectors, nil
}
