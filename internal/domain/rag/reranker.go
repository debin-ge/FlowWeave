package rag

import (
	applog "flowweave/internal/platform/log"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"flowweave/internal/provider"
)

// ── Reranker 接口 ─────────────────────────────────────────────

// Reranker 重排序接口
type Reranker interface {
	// Rerank 对候选文档按与 query 的相关性重新排序
	Rerank(ctx context.Context, query string, docs []ResultDocument, topK int) ([]ResultDocument, error)
}

// ── LLM Prompt-based Reranker 实现 ───────────────────────────

// LLMReranker 使用 LLM 做 prompt-based 相关性重排
type LLMReranker struct {
	providerName string
	model        string
}

// NewLLMReranker 创建 LLM Reranker
func NewLLMReranker(providerName, model string) *LLMReranker {
	return &LLMReranker{
		providerName: providerName,
		model:        model,
	}
}

// Rerank 使用 LLM 对文档做相关性评分并重排
func (r *LLMReranker) Rerank(ctx context.Context, query string, docs []ResultDocument, topK int) ([]ResultDocument, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	if topK <= 0 || topK > len(docs) {
		topK = len(docs)
	}

	start := time.Now()

	p, err := provider.GetProvider(r.providerName)
	if err != nil {
		return nil, fmt.Errorf("get provider %s: %w", r.providerName, err)
	}

	// 构建评分 prompt
	prompt := r.buildRerankPrompt(query, docs)

	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: r.model,
		Messages: []provider.Message{
			{Role: "system", Content: "你是文档相关性评分专家。根据用户查询，对每个文档给出 0.0-1.0 的相关性分数。仅返回 JSON 数组，不要输出其他内容。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
		MaxTokens:   512,
	})
	if err != nil {
		applog.Warn("[RAG/Reranker] LLM rerank failed, returning original order", "error", err)
		return docs[:topK], nil
	}

	// 解析评分结果
	scores, err := r.parseScores(resp.Content, len(docs))
	if err != nil {
		applog.Warn("[RAG/Reranker] Failed to parse scores, returning original order", "error", err, "response", resp.Content)
		return docs[:topK], nil
	}

	// 将 LLM 评分与文档关联并排序
	type scoredDoc struct {
		doc   ResultDocument
		score float64
	}
	scored := make([]scoredDoc, len(docs))
	for i, doc := range docs {
		s := doc.Score // 保留原始分数作为 fallback
		if i < len(scores) {
			s = scores[i]
		}
		scored[i] = scoredDoc{doc: doc, score: s}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]ResultDocument, 0, topK)
	for i := 0; i < topK && i < len(scored); i++ {
		doc := scored[i].doc
		doc.Score = scored[i].score
		result = append(result, doc)
	}

	applog.Info("[RAG/Reranker] Reranked",
		"input_docs", len(docs),
		"output_docs", len(result),
		"elapsed_ms", time.Since(start).Milliseconds(),
	)

	return result, nil
}

// buildRerankPrompt 构建评分 prompt
func (r *LLMReranker) buildRerankPrompt(query string, docs []ResultDocument) string {
	prompt := fmt.Sprintf("查询：%s\n\n请为以下 %d 个文档片段评分（0.0-1.0），返回 JSON 数组 [score1, score2, ...]：\n\n", query, len(docs))
	for i, doc := range docs {
		content := doc.Content
		// 截断过长内容
		if len([]rune(content)) > 300 {
			content = string([]rune(content)[:300]) + "..."
		}
		prompt += fmt.Sprintf("[文档 %d]\n%s\n\n", i+1, content)
	}
	return prompt
}

// parseScores 解析 LLM 返回的评分 JSON
func (r *LLMReranker) parseScores(content string, expectedCount int) ([]float64, error) {
	var scores []float64
	if err := json.Unmarshal([]byte(content), &scores); err != nil {
		// 尝试提取 JSON 数组部分
		start := -1
		for i, ch := range content {
			if ch == '[' {
				start = i
				break
			}
		}
		if start >= 0 {
			end := -1
			for i := len(content) - 1; i >= start; i-- {
				if content[i] == ']' {
					end = i + 1
					break
				}
			}
			if end > start {
				if err2 := json.Unmarshal([]byte(content[start:end]), &scores); err2 == nil {
					if len(scores) >= expectedCount {
						return scores[:expectedCount], nil
					}
					return scores, nil
				}
			}
		}
		return nil, fmt.Errorf("parse scores: %w", err)
	}
	if len(scores) > expectedCount {
		scores = scores[:expectedCount]
	}
	return scores, nil
}
