package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"flowweave/internal/provider"
)

// GatewayCompressor Context-Gateway 压缩器
type GatewayCompressor struct {
	providerName string         // LLM provider 名称
	modelName    string         // 压缩用模型
	estimator    TokenEstimator // Token 估算器
}

// NewGatewayCompressor 创建 Context-Gateway 压缩器
func NewGatewayCompressor(providerName, modelName string, estimator TokenEstimator) *GatewayCompressor {
	if estimator == nil {
		estimator = &SimpleTokenEstimator{}
	}
	applog.Info("[Gateway] Compressor initialized",
		"provider", providerName,
		"model", modelName,
	)
	return &GatewayCompressor{
		providerName: providerName,
		modelName:    modelName,
		estimator:    estimator,
	}
}

// GetEstimator 返回 Token 估算器
func (g *GatewayCompressor) GetEstimator() TokenEstimator {
	return g.estimator
}

// Compress 执行 Context-Gateway 压缩
func (g *GatewayCompressor) Compress(
	ctx context.Context,
	existingSummary string,
	messagesToCompress []provider.Message,
	existingFacts map[string]string,
	extractKeyFacts bool,
) (*CompressResult, error) {
	applog.Info("[Gateway] 🚀 Starting compression",
		"provider", g.providerName,
		"model", g.modelName,
		"messages_to_compress", len(messagesToCompress),
		"has_existing_summary", existingSummary != "",
		"existing_facts_count", len(existingFacts),
		"extract_key_facts", extractKeyFacts,
	)

	llmProvider, err := provider.GetProvider(g.providerName)
	if err != nil {
		applog.Error("[Gateway] ❌ Failed to get provider", "provider", g.providerName, "error", err)
		return nil, fmt.Errorf("get gateway provider: %w", err)
	}

	// 构建压缩 Prompt
	prompt := g.buildCompressPrompt(existingSummary, messagesToCompress, extractKeyFacts)

	req := &provider.CompletionRequest{
		Model: g.modelName,
		Messages: []provider.Message{
			{Role: "system", Content: gatewaySystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.2, // 低温度保证稳定压缩
		MaxTokens:   800,
	}

	resp, err := llmProvider.Complete(ctx, req)
	if err != nil {
		applog.Error("[Gateway] ❌ LLM compression failed", "error", err)
		return nil, fmt.Errorf("gateway compression failed: %w", err)
	}

	// 解析结果
	result, err := g.parseCompressResponse(resp.Content)
	if err != nil {
		applog.Warn("[Gateway] ⚠️ Failed to parse structured response, using raw text as summary",
			"error", err,
		)
		// 降级：将整个响应作为 summary
		result = &CompressResult{
			CompressedSummary: strings.TrimSpace(resp.Content),
			KeyFacts:          existingFacts,
		}
	} else {
		// 合并已有 Key Facts（新的覆盖旧的）
		if result.KeyFacts == nil {
			result.KeyFacts = make(map[string]string)
		}
		for k, v := range existingFacts {
			if _, exists := result.KeyFacts[k]; !exists {
				result.KeyFacts[k] = v
			}
		}

		// 限制 Key Facts 数量（最多 20 条）
		if len(result.KeyFacts) > 20 {
			trimmed := make(map[string]string)
			count := 0
			for k, v := range result.KeyFacts {
				if count >= 20 {
					break
				}
				trimmed[k] = v
				count++
			}
			result.KeyFacts = trimmed
		}
	}

	summaryPreview := result.CompressedSummary
	if len(summaryPreview) > 200 {
		summaryPreview = summaryPreview[:200] + "..."
	}
	applog.Info("[Gateway] ✅ Compression completed",
		"summary_length", len(result.CompressedSummary),
		"key_facts_count", len(result.KeyFacts),
		"summary_preview", summaryPreview,
	)

	return result, nil
}

// SelectMessagesForCompression 分割待压缩和保留的消息
func SelectMessagesForCompression(messages []provider.Message, minRecentTurns int) (toCompress, toKeep []provider.Message) {
	minRecentMsgs := minRecentTurns * 2
	if len(messages) <= minRecentMsgs {
		return nil, messages // 不够压缩
	}
	cutoff := len(messages) - minRecentMsgs
	return messages[:cutoff], messages[cutoff:]
}

const gatewaySystemPrompt = `你是一个对话上下文压缩引擎。你的任务是将对话记忆重写为紧凑形式，同时保留关键信息。
要求：
1. 保留任务目标、约束条件、已做出的决策和结论
2. 保留关键事实和数据
3. 保留用户的偏好和需求
4. 删除闲聊、重复信息和无关细节
5. 输出必须是有效的 JSON 格式`

func (g *GatewayCompressor) buildCompressPrompt(
	existingSummary string,
	messages []provider.Message,
	extractKeyFacts bool,
) string {
	var sb strings.Builder

	if existingSummary != "" {
		sb.WriteString("已有记忆：\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n")
	}

	sb.WriteString("新增对话消息：\n")
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("用户: %s\n", msg.Content))
		case "assistant":
			sb.WriteString(fmt.Sprintf("助手: %s\n", msg.Content))
		}
	}

	sb.WriteString("\n请将以上内容压缩为紧凑的记忆摘要。\n")

	if extractKeyFacts {
		sb.WriteString("同时请提取关键事实（key-value 格式，如 \"目的地\": \"东京\"）。\n")
		sb.WriteString("\n请按以下 JSON 格式输出：\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"compressed_summary\": \"...\",\n")
		sb.WriteString("  \"key_facts\": {\"key\": \"value\", ...}\n")
		sb.WriteString("}\n")
	} else {
		sb.WriteString("\n请按以下 JSON 格式输出：\n")
		sb.WriteString("{\n")
		sb.WriteString("  \"compressed_summary\": \"...\"\n")
		sb.WriteString("}\n")
	}

	return sb.String()
}

func (g *GatewayCompressor) parseCompressResponse(response string) (*CompressResult, error) {
	// 尝试从响应中提取 JSON
	response = strings.TrimSpace(response)

	// 移除可能的 markdown 代码块包裹
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		response = strings.TrimSuffix(response, "```")
		response = strings.TrimSpace(response)
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
		response = strings.TrimSpace(response)
	}

	var result CompressResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("parse compress response: %w", err)
	}

	if result.CompressedSummary == "" {
		return nil, fmt.Errorf("compressed_summary is empty")
	}

	return &result, nil
}
