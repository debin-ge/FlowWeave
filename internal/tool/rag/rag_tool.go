package ragtool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"flowweave/internal/domain/rag"
)

// RAGTool 知识库检索工具
type RAGTool struct {
	retriever      *rag.Retriever
	datasetIDs     []string // DSL 静态参数：限定检索范围
	topK           int
	scoreThreshold float64
	mode           rag.RetrievalMode
}

// RAGToolConfig RAG 工具配置（从 DSL args 解析）
type RAGToolConfig struct {
	DatasetIDs     []string `json:"dataset_ids,omitempty"`
	TopK           int      `json:"top_k,omitempty"`
	ScoreThreshold float64  `json:"score_threshold,omitempty"`
	Mode           string   `json:"mode,omitempty"` // BM25 / HYBRID / AUTO
}

// NewRAGTool 创建知识库检索工具
func NewRAGTool(retriever *rag.Retriever, config *RAGToolConfig) *RAGTool {
	t := &RAGTool{
		retriever: retriever,
		topK:      5,
		mode:      rag.RetrievalModeBM25,
	}
	if config != nil {
		if len(config.DatasetIDs) > 0 {
			t.datasetIDs = config.DatasetIDs
		}
		if config.TopK > 0 {
			t.topK = config.TopK
		}
		if config.ScoreThreshold > 0 {
			t.scoreThreshold = config.ScoreThreshold
		}
		if config.Mode != "" {
			if mode, err := parseRetrievalMode(config.Mode); err == nil {
				t.mode = mode
			}
		}
	}
	return t
}

func (t *RAGTool) Name() string {
	return "knowledge_search"
}

func (t *RAGTool) Description() string {
	// 描述由 DSL tools[].description 提供
	return ""
}

func (t *RAGTool) Parameters() interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "用于搜索知识库的查询文本",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "返回的最大结果数量，默认 5",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "检索模式：BM25 或 HYBRID（默认 BM25）",
				"enum":        []string{"BM25", "HYBRID"},
			},
		},
		"required": []string{"query"},
	}
}

// ragToolArguments LLM 传入的动态参数
type ragToolArguments struct {
	Query          string   `json:"query"`
	TopK           int      `json:"top_k,omitempty"`
	DatasetIDs     []string `json:"dataset_ids,omitempty"`
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
	Mode           string   `json:"mode,omitempty"`
}

// Execute 执行知识库检索
func (t *RAGTool) Execute(ctx context.Context, arguments string) (string, error) {
	// 1. 解析 LLM 传入的动态参数
	var args ragToolArguments
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if t.retriever == nil {
		return "", fmt.Errorf("retriever is not configured")
	}

	// 2. 合并参数优先级：LLM 动态 > DSL 静态 > 默认值
	topK := t.topK
	if args.TopK > 0 {
		topK = args.TopK
	}
	datasetIDs := t.datasetIDs
	if len(args.DatasetIDs) > 0 {
		datasetIDs = args.DatasetIDs
	}
	scoreThreshold := t.scoreThreshold
	if args.ScoreThreshold != nil {
		scoreThreshold = *args.ScoreThreshold
	}
	mode := t.mode
	if args.Mode != "" {
		parsedMode, err := parseRetrievalMode(args.Mode)
		if err != nil {
			return "", err
		}
		mode = parsedMode
	}

	// 3. 构建检索请求
	searchReq := &rag.SearchRequest{
		Query:          args.Query,
		TopK:           topK,
		ScoreThreshold: scoreThreshold,
		DatasetIDs:     datasetIDs,
		Mode:           mode,
	}

	// 注入多租户 scope
	if scope := rag.GetScopeFromContext(ctx); scope != nil {
		searchReq.OrgID = scope.OrgID
		searchReq.TenantID = scope.TenantID
	}

	// 4. 执行检索
	result, err := t.retriever.Search(ctx, searchReq)
	if err != nil {
		return "", fmt.Errorf("knowledge search failed: %w", err)
	}

	// 5. 格式化结果
	if len(result.Documents) == 0 {
		return "未找到与查询相关的文档。", nil
	}

	return rag.FormatContext(result), nil
}

// ParseRAGToolConfig 从 DSL args map 解析 RAG 工具配置
func ParseRAGToolConfig(args map[string]interface{}) *RAGToolConfig {
	config := &RAGToolConfig{}

	if ids, ok := args["dataset_ids"]; ok {
		if idList, ok := ids.([]interface{}); ok {
			for _, id := range idList {
				if s, ok := id.(string); ok {
					config.DatasetIDs = append(config.DatasetIDs, s)
				}
			}
		}
	}

	if topK, ok := args["top_k"]; ok {
		switch v := topK.(type) {
		case float64:
			config.TopK = int(v)
		case int:
			config.TopK = v
		}
	}

	if threshold, ok := args["score_threshold"]; ok {
		if v, ok := threshold.(float64); ok {
			config.ScoreThreshold = v
		}
	}
	if mode, ok := args["mode"].(string); ok {
		config.Mode = mode
	}

	return config
}

func parseRetrievalMode(mode string) (rag.RetrievalMode, error) {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case string(rag.RetrievalModeBM25):
		return rag.RetrievalModeBM25, nil
	case string(rag.RetrievalModeHybrid):
		return rag.RetrievalModeHybrid, nil
	case string(rag.RetrievalModeAuto):
		return rag.RetrievalModeAuto, nil
	default:
		return "", fmt.Errorf("invalid retrieval mode: %s", mode)
	}
}
