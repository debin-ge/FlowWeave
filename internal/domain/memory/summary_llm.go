package memory

import (
	applog "flowweave/internal/platform/log"
	"context"
	"fmt"
	"strings"

	"flowweave/internal/provider"
)

// LLMSummaryGenerator 使用 LLM 生成对话摘要
type LLMSummaryGenerator struct {
	providerName string // LLM provider 名称
	modelName    string // 模型名称
}

// NewLLMSummaryGenerator 创建 LLM 摘要生成器
func NewLLMSummaryGenerator(providerName, modelName string) *LLMSummaryGenerator {
	applog.Info("[Summary/LLM] Generator initialized",
		"provider", providerName,
		"model", modelName,
	)
	return &LLMSummaryGenerator{
		providerName: providerName,
		modelName:    modelName,
	}
}

// Summarize 根据对话消息生成/更新摘要
func (g *LLMSummaryGenerator) Summarize(ctx context.Context, messages []Message, existingSummary string) (string, error) {
	applog.Info("[Summary/LLM] 🤖 Starting summary generation",
		"provider", g.providerName,
		"model", g.modelName,
		"message_count", len(messages),
		"has_existing_summary", existingSummary != "",
	)

	llmProvider, err := provider.GetProvider(g.providerName)
	if err != nil {
		applog.Error("[Summary/LLM] ❌ Failed to get provider",
			"provider", g.providerName,
			"error", err,
		)
		return "", fmt.Errorf("get summary provider: %w", err)
	}

	// 构建摘要提示
	prompt := g.buildSummaryPrompt(messages, existingSummary)

	promptPreview := prompt
	if len(promptPreview) > 300 {
		promptPreview = promptPreview[:300] + "..."
	}
	applog.Debug("[Summary/LLM] Prompt built",
		"prompt_length", len(prompt),
		"prompt_preview", promptPreview,
	)

	req := &provider.CompletionRequest{
		Model: g.modelName,
		Messages: []provider.Message{
			{Role: "system", Content: summarySystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3, // 低温度保证稳定摘要
		MaxTokens:   500,
	}

	applog.Debug("[Summary/LLM] Calling LLM...",
		"model", g.modelName,
		"temperature", req.Temperature,
		"max_tokens", req.MaxTokens,
	)

	resp, err := llmProvider.Complete(ctx, req)
	if err != nil {
		applog.Error("[Summary/LLM] ❌ LLM call failed",
			"provider", g.providerName,
			"model", g.modelName,
			"error", err,
		)
		return "", fmt.Errorf("summary generation failed: %w", err)
	}

	result := strings.TrimSpace(resp.Content)

	resultPreview := result
	if len(resultPreview) > 300 {
		resultPreview = resultPreview[:300] + "..."
	}
	applog.Info("[Summary/LLM] ✅ Summary generated",
		"provider", g.providerName,
		"model", g.modelName,
		"summary_length", len(result),
		"summary_preview", resultPreview,
	)

	return result, nil
}

const summarySystemPrompt = `你是一个对话摘要助手。你的任务是将对话内容压缩为简洁的摘要，保留关键信息和上下文。
要求：
1. 保留对话中的关键事实、决策和结论
2. 保留用户的偏好和需求
3. 使用简洁的语言，避免冗余
4. 如果提供了已有摘要，请在其基础上融合新对话内容，生成更新版本的摘要
5. 摘要应当使用第三人称`

func (g *LLMSummaryGenerator) buildSummaryPrompt(messages []Message, existingSummary string) string {
	var sb strings.Builder

	if existingSummary != "" {
		sb.WriteString("## 已有摘要\n\n")
		sb.WriteString(existingSummary)
		sb.WriteString("\n\n## 新增对话\n\n")
	} else {
		sb.WriteString("## 对话内容\n\n")
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("用户: %s\n", msg.Content))
		case "assistant":
			sb.WriteString(fmt.Sprintf("助手: %s\n", msg.Content))
		}
	}

	if existingSummary != "" {
		sb.WriteString("\n请在已有摘要的基础上，融合新增对话内容，生成一份更新后的完整摘要。")
	} else {
		sb.WriteString("\n请为以上对话生成一份简洁的摘要。")
	}

	return sb.String()
}
