package llm

import (
	"context"
	"encoding/json"
	applog "flowweave/internal/platform/log"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"flowweave/internal/adapter/provider/llm"
	"flowweave/internal/domain/memory"
	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/port"
	"flowweave/internal/tool"
)

// LLMNodeData LLM 节点配置数据
type LLMNodeData struct {
	Type    string               `json:"type"`
	Title   string               `json:"title"`
	Model   ModelConfig          `json:"model"`
	Prompts []PromptTemplate     `json:"prompts"`
	Memory  *memory.MemoryConfig `json:"memory,omitempty"`
	Vision  *VisionConfig        `json:"vision,omitempty"`
	Tools   []ToolBinding        `json:"tools,omitempty"` // Agent 工具绑定列表
}

// ToolBinding DSL 中单个工具的绑定配置
type ToolBinding struct {
	Name        string                 `json:"name"`                  // 工具名称，对应 tool.Registry 中的 key
	Description string                 `json:"description,omitempty"` // DSL 可覆盖工具描述
	Args        map[string]interface{} `json:"args,omitempty"`        // 静态参数（如 dataset_ids、top_k）
}

// ModelConfig 模型配置
type ModelConfig struct {
	Provider    string  `json:"provider"` // openai, deepseek, etc.
	Name        string  `json:"name"`     // gpt-4, deepseek-chat, etc.
	Mode        string  `json:"mode"`     // chat, completion
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	TopP        float64 `json:"top_p"`
}

// PromptTemplate 提示词模板
type PromptTemplate struct {
	Role string `json:"role"` // system, user, assistant
	Text string `json:"text"` // 支持 {{#node_id.var_name#}} 模板变量
}

// VisionConfig 视觉配置
type VisionConfig struct {
	Enabled bool `json:"enabled"`
}

// LLMNode LLM 节点
type LLMNode struct {
	*node.BaseNode
	data LLMNodeData
}

func init() {
	node.Register(types.NodeTypeLLM, NewLLMNode)
}

// NewLLMNode 创建 LLM 节点
func NewLLMNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data LLMNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, fmt.Errorf("parse LLM node data: %w", err)
	}

	// 验证记忆配置依赖关系
	if data.Memory != nil {
		if err := data.Memory.Validate(); err != nil {
			return nil, fmt.Errorf("invalid memory config: %w", err)
		}
	}

	// 验证工具配置：description 由 DSL 提供，不走代码内硬编码
	for i, tb := range data.Tools {
		if strings.TrimSpace(tb.Name) == "" {
			return nil, fmt.Errorf("invalid tool binding at index %d: name is required", i)
		}
		if strings.TrimSpace(tb.Description) == "" {
			return nil, fmt.Errorf("invalid tool binding %q: description is required in DSL", tb.Name)
		}
	}

	n := &LLMNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeLLM, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
	}
	return n, nil
}

// Run 执行 LLM 节点（支持 Agent Tool Calling 循环）
func (n *LLMNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunStreamWithEvents(ctx, n, func(ctx context.Context, stream chan<- string) (*node.NodeRunResult, error) {
		// 1. 获取 LLM provider
		llmProvider, err := provider.GetProvider(n.data.Model.Provider)
		if err != nil {
			return nil, fmt.Errorf("get LLM provider: %w", err)
		}

		// 2. 构建消息列表（解析模板变量）
		vp, _ := node.GetVariablePoolFromContext(ctx)
		messages := n.buildMessages(vp)

		// 3. 如果启用了记忆，在 prompt messages 中插入历史对话
		var userInput string
		messages, userInput = n.injectMemory(ctx, messages)

		// 4. 构建工具定义（如果 DSL 配置了 tools）
		var toolDefs []provider.ToolDefinition
		var toolRegistry *tool.Registry
		if len(n.data.Tools) > 0 {
			toolRegistry, _ = tool.RegistryFromContext(ctx)
			if toolRegistry != nil {
				toolDefs = n.buildToolDefinitions(toolRegistry)
				names := make([]string, 0, len(toolDefs))
				for _, def := range toolDefs {
					names = append(names, def.Function.Name)
				}
				applog.Info("[LLM/Agent] Tools configured",
					"node_id", n.ID(),
					"tool_count", len(toolDefs),
					"tool_names", names,
				)
			}
		}

		// 记录初始请求消息（用于溯源）
		traceMessages := make([]map[string]string, len(messages))
		for i, m := range messages {
			traceMessages[i] = map[string]string{
				"role":    m.Role,
				"content": m.Content,
			}
		}

		callStart := time.Now()

		// 5. Agent 循环：如果配置了工具，进入循环模式
		var content string
		var totalTokens int
		const safetyLimit = 10

		if len(toolDefs) > 0 && toolRegistry != nil {
			// === Agent 模式 ===
			for round := 0; round < safetyLimit; round++ {
				req := &provider.CompletionRequest{
					Model:       n.data.Model.Name,
					Messages:    messages,
					Temperature: n.data.Model.Temperature,
					MaxTokens:   n.data.Model.MaxTokens,
					TopP:        n.data.Model.TopP,
					Tools:       toolDefs,
					ToolChoice:  "auto",
				}

				// 非流式调用（中间轮次不需要流式输出）
				resp, err := llmProvider.Complete(ctx, req)
				if err != nil {
					return nil, fmt.Errorf("LLM complete error (round %d): %w", round, err)
				}
				totalTokens += resp.Usage.TotalTokens

				// ✅ 核心判断：没有 tool_calls 就是最终答案
				if len(resp.ToolCalls) == 0 {
					content = resp.Content
					// 流式输出最终答案
					if content != "" {
						stream <- content
					}
					applog.Info("[LLM/Agent] Final answer (no more tool calls)",
						"node_id", n.ID(),
						"rounds", round+1,
						"total_tokens", totalTokens,
					)
					break
				}

				// LLM 返回了 tool_calls，执行工具
				applog.Info("[LLM/Agent] Tool calls received",
					"node_id", n.ID(),
					"round", round+1,
					"tool_count", len(resp.ToolCalls),
				)

				// 追加 assistant 消息（携带 tool_calls）
				messages = append(messages, provider.Message{
					Role:      "assistant",
					Content:   resp.Content,
					ToolCalls: resp.ToolCalls,
				})

				// 并行执行工具调用，按原始顺序回填 tool 消息
				toolMessages := make([]provider.Message, len(resp.ToolCalls))
				var toolWG sync.WaitGroup
				for i, tc := range resp.ToolCalls {
					i, tc := i, tc
					toolWG.Add(1)
					go func() {
						defer toolWG.Done()

						applog.Info("[LLM/Agent] Executing tool",
							"tool", tc.Function.Name,
							"call_id", tc.ID,
							"arguments", tc.Function.Arguments,
						)

						// 合并 DSL 静态 args + LLM 动态 arguments
						execArgs := n.mergeToolArgs(tc.Function.Name, tc.Function.Arguments)

						toolResult, toolErr := toolRegistry.Execute(ctx, tc.Function.Name, execArgs)
						if toolErr != nil {
							toolResult = fmt.Sprintf("工具执行失败: %s", toolErr.Error())
							applog.Error("[LLM/Agent] Tool execution failed",
								"tool", tc.Function.Name,
								"error", toolErr,
							)
						} else {
							resultPreview := toolResult
							if len(resultPreview) > 200 {
								resultPreview = resultPreview[:200] + "..."
							}
							applog.Info("[LLM/Agent] Tool result",
								"tool", tc.Function.Name,
								"result_preview", resultPreview,
							)
						}

						toolMessages[i] = provider.Message{
							Role:       "tool",
							Content:    toolResult,
							ToolCallID: tc.ID,
							Name:       tc.Function.Name,
						}
					}()
				}
				toolWG.Wait()
				messages = append(messages, toolMessages...)
				// 继续循环，带着工具结果再次调用 LLM
			}

			if content == "" {
				return nil, fmt.Errorf("exceeded safety limit (%d) for tool call rounds", safetyLimit)
			}
		} else {
			// === 普通模式（无工具，直接流式输出） ===
			req := &provider.CompletionRequest{
				Model:       n.data.Model.Name,
				Messages:    messages,
				Temperature: n.data.Model.Temperature,
				MaxTokens:   n.data.Model.MaxTokens,
				TopP:        n.data.Model.TopP,
			}

			chunkCh, errCh := llmProvider.StreamComplete(ctx, req)

			var contentBuilder strings.Builder

		loop:
			for {
				select {
				case chunk, ok := <-chunkCh:
					if !ok {
						break loop
					}
					if chunk.Delta != "" {
						contentBuilder.WriteString(chunk.Delta)
						stream <- chunk.Delta
					}
				case err, ok := <-errCh:
					if ok && err != nil {
						return nil, fmt.Errorf("LLM stream error: %w", err)
					}
					if !ok {
						break loop
					}
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			content = contentBuilder.String()
		}

		callElapsed := time.Since(callStart).Milliseconds()

		// 6. 如果启用了记忆，保存本轮对话
		n.saveMemory(ctx, userInput, content)

		return &node.NodeRunResult{
			Status: types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{
				"text": content,
			},
			Metadata: map[string]interface{}{
				"provider":     n.data.Model.Provider,
				"model":        n.data.Model.Name,
				"total_tokens": totalTokens,
				"llm_trace": map[string]interface{}{
					"provider":    n.data.Model.Provider,
					"model":       n.data.Model.Name,
					"messages":    traceMessages,
					"temperature": n.data.Model.Temperature,
					"max_tokens":  n.data.Model.MaxTokens,
					"top_p":       n.data.Model.TopP,
					"response":    content,
					"elapsed_ms":  callElapsed,
				},
			},
		}, nil
	})
}

// buildToolDefinitions 根据 DSL tools 构建 provider 工具定义。
// description 由 DSL 提供，不回退到工具实现中的默认值。
func (n *LLMNode) buildToolDefinitions(reg *tool.Registry) []provider.ToolDefinition {
	defs := make([]provider.ToolDefinition, 0, len(n.data.Tools))
	for _, tb := range n.data.Tools {
		if tb.Name == "" {
			continue
		}

		t, ok := reg.Get(tb.Name)
		if !ok {
			applog.Warn("[LLM/Agent] Tool not found in registry",
				"node_id", n.ID(),
				"tool", tb.Name,
			)
			continue
		}

		defs = append(defs, provider.ToolDefinition{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        t.Name(),
				Description: strings.TrimSpace(tb.Description),
				Parameters:  t.Parameters(),
			},
		})
	}
	return defs
}

// extractUserInput 从消息列表提取当前用户输入（最后一条非 system 消息）
func extractUserInput(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "system" {
			return messages[i].Content
		}
	}
	return ""
}

// injectMemory 如果启用了记忆，将历史对话插入消息列表
// 组装顺序：system → key_facts → mid_term_summary → gateway_summary → recent_messages → user_input
// 返回：完整消息列表 + 当前用户输入文本
func (n *LLMNode) injectMemory(ctx context.Context, messages []provider.Message) ([]provider.Message, string) {
	// 无论是否注入记忆，都先提取当前用户输入（用于后续 saveMemory）
	userInput := extractUserInput(messages)

	if n.data.Memory == nil || !n.data.Memory.IsShortTermEnabled() {
		applog.Debug("[LLM/Memory] Memory not enabled, skipping injection", "node_id", n.ID())
		return messages, userInput
	}

	coordinator, ok := memory.CoordinatorFromContext(ctx)
	if !ok {
		applog.Warn("[LLM/Memory] ⚠️ No coordinator in context", "node_id", n.ID())
		return messages, userInput
	}

	convID := memory.ConversationIDFromContext(ctx)
	if convID == "" {
		applog.Warn("[LLM/Memory] ⚠️ No conversation_id in context", "node_id", n.ID())
		return messages, userInput
	}

	applog.Info("[LLM/Memory] 🔄 Starting memory injection",
		"node_id", n.ID(),
		"conversation_id", convID,
		"original_messages", len(messages),
		"current_user_input", userInput,
	)

	// 回忆历史
	recall, err := coordinator.Recall(ctx, &memory.RecallRequest{
		ConversationID: convID,
		Config:         n.data.Memory,
	})
	if err != nil {
		applog.Warn("[LLM/Memory] ⚠️ Recall failed", "node_id", n.ID(), "error", err)
		return messages, userInput
	}

	// 如果没有任何记忆数据，直接返回
	if len(recall.ShortTermMessages) == 0 && recall.MidTermSummary == "" &&
		recall.GatewaySummary == "" && len(recall.KeyFacts) == 0 {
		applog.Debug("[LLM/Memory] No memory data found, using original messages",
			"node_id", n.ID(),
			"conversation_id", convID,
		)
		return messages, userInput
	}

	// 提取 system prompts
	var systemMsgs []provider.Message
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		}
	}

	// 组装消息顺序：system → key_facts → mid_term_summary → gateway_summary → recent → user_input
	result := make([]provider.Message, 0, len(systemMsgs)+len(recall.ShortTermMessages)+4)

	// 1. System prompts
	result = append(result, systemMsgs...)

	// 2. Key Facts（如有）
	if len(recall.KeyFacts) > 0 {
		factsContent := formatKeyFacts(recall.KeyFacts)
		result = append(result, provider.Message{
			Role:    "system",
			Content: "## 关键事实\n" + factsContent,
		})
	}

	// 3. Mid-term Summary（阶段性记忆摘要）
	if recall.MidTermSummary != "" {
		result = append(result, provider.Message{
			Role:    "system",
			Content: "## 阶段性记忆摘要\n" + recall.MidTermSummary,
		})
	}

	// 4. Gateway Summary（运行时压缩摘要）
	if recall.GatewaySummary != "" {
		result = append(result, provider.Message{
			Role:    "system",
			Content: "## 运行时压缩摘要\n" + recall.GatewaySummary,
		})
	}

	// 5. Recent Messages
	result = append(result, recall.ShortTermMessages...)

	// 6. Current User Input
	if userInput != "" {
		result = append(result, provider.Message{Role: "user", Content: userInput})
	}

	userPreview := userInput
	if len(userPreview) > 100 {
		userPreview = userPreview[:100] + "..."
	}

	applog.Info("[LLM/Memory] ✅ Memory injected into messages",
		"node_id", n.ID(),
		"conversation_id", convID,
		"system_msgs", len(systemMsgs),
		"has_key_facts", len(recall.KeyFacts) > 0,
		"has_gateway_summary", recall.GatewaySummary != "",
		"has_mtm_summary", recall.MidTermSummary != "",
		"stm_history_msgs", len(recall.ShortTermMessages),
		"total_assembled_msgs", len(result),
		"current_user_input", userPreview,
	)

	// Debug: 打印完整消息列表
	for i, msg := range result {
		contentPreview := msg.Content
		if len(contentPreview) > 80 {
			contentPreview = contentPreview[:80] + "..."
		}
		applog.Debug("[LLM/Memory] Assembled message",
			"index", i,
			"role", msg.Role,
			"content_preview", contentPreview,
		)
	}

	applog.Info("[LLM/Memory] 🔄 Memory injection completed",
		"node_id", n.ID(),
		"conversation_id", convID,
		"total_assembled_msgs", len(result),
		"stm_history_msgs", result,
	)

	return result, userInput
}

// formatKeyFacts 将 Key Facts 格式化为可读文本
func formatKeyFacts(facts map[string]string) string {
	if len(facts) == 0 {
		return ""
	}
	// 按 key 字母序排列以保证稳定输出
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", k, facts[k]))
	}
	return sb.String()
}

// saveMemory 保存本轮对话到记忆
func (n *LLMNode) saveMemory(ctx context.Context, userInput, assistantOutput string) {
	if n.data.Memory == nil || !n.data.Memory.IsShortTermEnabled() {
		return
	}

	coordinator, ok := memory.CoordinatorFromContext(ctx)
	if !ok {
		applog.Warn("[LLM/Memory] ⚠️ No coordinator for saveMemory", "node_id", n.ID())
		return
	}

	convID := memory.ConversationIDFromContext(ctx)
	if convID == "" {
		applog.Warn("[LLM/Memory] ⚠️ No conversation_id for saveMemory", "node_id", n.ID())
		return
	}

	userPreview := userInput
	if len(userPreview) > 100 {
		userPreview = userPreview[:100] + "..."
	}
	assistantPreview := assistantOutput
	if len(assistantPreview) > 100 {
		assistantPreview = assistantPreview[:100] + "..."
	}

	applog.Info("[LLM/Memory] 💾 Saving conversation turn",
		"node_id", n.ID(),
		"conversation_id", convID,
		"user_preview", userPreview,
		"assistant_preview", assistantPreview,
	)

	memReq := &memory.MemorizeRequest{
		ConversationID: convID,
		Config:         n.data.Memory,
		UserMessage:    provider.Message{Role: "user", Content: userInput},
		AssistantMsg:   provider.Message{Role: "assistant", Content: assistantOutput},
	}
	go func(req *memory.MemorizeRequest) {
		asyncCtx := context.Background()
		if orgID, tenantID, ok := port.RepoScopeFrom(ctx); ok {
			asyncCtx = port.WithRepoScope(asyncCtx, orgID, tenantID)
		}

		err := coordinator.Memorize(asyncCtx, req)
		if err != nil {
			applog.Warn("[LLM/Memory] ⚠️ Memorize failed",
				"node_id", n.ID(),
				"conversation_id", convID,
				"error", err,
			)
		} else {
			applog.Info("[LLM/Memory] ✅ Conversation turn saved",
				"node_id", n.ID(),
				"conversation_id", convID,
			)
		}
	}(memReq)
}

// buildMessages 从模板构建消息列表
func (n *LLMNode) buildMessages(vp node.VariablePoolAccessor) []provider.Message {
	messages := make([]provider.Message, 0, len(n.data.Prompts))

	for _, prompt := range n.data.Prompts {
		text := prompt.Text

		// 解析模板变量 {{#node_id.var_name#}}
		if vp != nil {
			text = resolveTemplate(text, vp)
		}

		messages = append(messages, provider.Message{
			Role:    prompt.Role,
			Content: text,
		})
	}

	return messages
}

// resolveTemplate 解析模板中的 {{#node_id.var_name#}} 引用
func resolveTemplate(template string, vp node.VariablePoolAccessor) string {
	var result strings.Builder
	i := 0
	for i < len(template) {
		if i+3 <= len(template) && template[i:i+3] == "{{#" {
			end := strings.Index(template[i+3:], "#}}")
			if end >= 0 {
				ref := template[i+3 : i+3+end]
				parts := strings.SplitN(ref, ".", 2)
				if len(parts) == 2 {
					val, ok := vp.GetVariable(types.VariableSelector(parts))
					if ok {
						result.WriteString(fmt.Sprintf("%v", val))
					}
				}
				i = i + 3 + end + 3
				continue
			}
		}
		result.WriteByte(template[i])
		i++
	}
	return result.String()
}

// mergeToolArgs 合并 DSL 静态 Args 和 LLM 动态 Arguments
// DSL args 作为基础，LLM 动态参数覆盖
func (n *LLMNode) mergeToolArgs(toolName string, llmArguments string) string {
	// 查找 DSL 中该工具的静态配置
	var dslArgs map[string]interface{}
	for _, tb := range n.data.Tools {
		if tb.Name == toolName {
			dslArgs = tb.Args
			break
		}
	}

	// 如果没有 DSL 静态参数，直接返回 LLM 参数
	if len(dslArgs) == 0 {
		return llmArguments
	}

	// 解析 LLM 动态参数
	var llmArgs map[string]interface{}
	if err := json.Unmarshal([]byte(llmArguments), &llmArgs); err != nil {
		// 解析失败时直接返回 LLM 原始参数
		return llmArguments
	}

	// 合并：DSL 为基础，LLM 参数优先覆盖
	merged := make(map[string]interface{})
	for k, v := range dslArgs {
		merged[k] = v
	}
	for k, v := range llmArgs {
		merged[k] = v
	}

	result, err := json.Marshal(merged)
	if err != nil {
		return llmArguments
	}
	return string(result)
}
