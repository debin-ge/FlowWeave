package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"flowweave/internal/provider"
)

// Config OpenAI 兼容 API 配置
type Config struct {
	APIKey                     string `json:"api_key"`
	BaseURL                    string `json:"base_url"` // 默认 https://api.openai.com/v1
	ConnectTimeoutSeconds      int    `json:"connect_timeout_seconds"`
	TLSHandshakeTimeoutSeconds int    `json:"tls_handshake_timeout_seconds"`
}

// Provider OpenAI 兼容的 LLM Provider
// 支持所有 OpenAI API 兼容服务（OpenAI, Azure, DeepSeek, Ollama 等）
type Provider struct {
	config Config
	client *http.Client
}

// New 创建 OpenAI 兼容 Provider
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	// 移除末尾斜杠
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")

	connectTimeout := time.Duration(config.ConnectTimeoutSeconds) * time.Second
	if connectTimeout <= 0 {
		connectTimeout = 30 * time.Second
	}
	tlsHandshakeTimeout := time.Duration(config.TLSHandshakeTimeoutSeconds) * time.Second
	if tlsHandshakeTimeout <= 0 {
		tlsHandshakeTimeout = 30 * time.Second
	}

	// Go 默认 Transport 的 TLS 握手超时为 10s，弱网下容易触发 handshake timeout。
	// 这里改为可配置，并保留通过 ctx 控制请求生命周期。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   connectTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = tlsHandshakeTimeout

	return &Provider{
		config: config,
		client: &http.Client{Transport: transport},
	}
}

func (p *Provider) Name() string {
	return "openai"
}

// -- 内部 API 请求/响应结构 --

type apiRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   *int         `json:"max_tokens,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Stream      bool         `json:"stream"`
	Tools       []apiToolDef `json:"tools,omitempty"`
	ToolChoice  interface{}  `json:"tool_choice,omitempty"`
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type apiToolDef struct {
	Type     string          `json:"type"`
	Function apiToolFunction `json:"function"`
}

type apiToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type apiToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function apiToolCallFunction `json:"function"`
	Index    *int                `json:"index,omitempty"` // SSE 流中标识工具调用索引
}

type apiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiResponse struct {
	ID      string      `json:"id"`
	Choices []apiChoice `json:"choices"`
	Usage   apiUsage    `json:"usage"`
	Model   string      `json:"model"`
}

type apiChoice struct {
	Message      apiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
	Delta        apiMessage `json:"delta"`
}

type apiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Complete 非流式补全
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	apiReq := p.buildAPIRequest(req, false)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := apiResp.Choices[0]
	result := &provider.CompletionResponse{
		Content:      choice.Message.Content,
		Model:        apiResp.Model,
		FinishReason: choice.FinishReason,
		Usage: provider.Usage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		},
	}

	// 解析 tool_calls
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]provider.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			result.ToolCalls[i] = provider.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: provider.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	return result, nil
}

// StreamComplete 流式补全
func (p *Provider) StreamComplete(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.CompletionChunk, <-chan error) {
	chunkCh := make(chan provider.CompletionChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		apiReq := p.buildAPIRequest(req, true)

		body, err := json.Marshal(apiReq)
		if err != nil {
			errCh <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		p.setHeaders(httpReq)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- fmt.Errorf("request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
			return
		}

		// 用于聚合跨多个 chunk 的 tool_calls 参数
		type toolCallAccumulator struct {
			ID          string
			Type        string
			Name        string
			ArgsBuilder strings.Builder
		}
		var toolCallAccumulators []toolCallAccumulator

		// 解析 SSE 流
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				// 如果有聚合的 tool_calls，在最后一个 chunk 中发出
				if len(toolCallAccumulators) > 0 {
					toolCalls := make([]provider.ToolCall, len(toolCallAccumulators))
					for i, acc := range toolCallAccumulators {
						toolCalls[i] = provider.ToolCall{
							ID:   acc.ID,
							Type: acc.Type,
							Function: provider.ToolCallFunction{
								Name:      acc.Name,
								Arguments: acc.ArgsBuilder.String(),
							},
						}
					}
					chunkCh <- provider.CompletionChunk{
						ToolCalls:    toolCalls,
						FinishReason: "tool_calls",
					}
				}
				return
			}

			var streamResp apiResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				continue
			}

			if len(streamResp.Choices) > 0 {
				choice := streamResp.Choices[0]

				// 处理 tool_calls delta（参数可能跨多个 chunk 到达）
				if len(choice.Delta.ToolCalls) > 0 {
					for _, tc := range choice.Delta.ToolCalls {
						idx := 0
						if tc.Index != nil {
							idx = *tc.Index
						}
						// 扩展 accumulator 数组
						for len(toolCallAccumulators) <= idx {
							toolCallAccumulators = append(toolCallAccumulators, toolCallAccumulator{})
						}
						// 首次出现时设置 ID/Type/Name
						if tc.ID != "" {
							toolCallAccumulators[idx].ID = tc.ID
						}
						if tc.Type != "" {
							toolCallAccumulators[idx].Type = tc.Type
						}
						if tc.Function.Name != "" {
							toolCallAccumulators[idx].Name = tc.Function.Name
						}
						// 追加参数片段
						if tc.Function.Arguments != "" {
							toolCallAccumulators[idx].ArgsBuilder.WriteString(tc.Function.Arguments)
						}
					}
					continue // tool_call delta 不产生文本 chunk
				}

				// 普通文本 delta
				chunk := provider.CompletionChunk{
					Delta:        choice.Delta.Content,
					FinishReason: choice.FinishReason,
				}
				chunkCh <- chunk
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("stream read error: %w", err)
		}
	}()

	return chunkCh, errCh
}

func (p *Provider) buildAPIRequest(req *provider.CompletionRequest, stream bool) apiRequest {
	messages := make([]apiMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := apiMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		// 转换 tool_calls
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]apiToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = apiToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: apiToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		messages[i] = msg
	}

	apiReq := apiRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   stream,
	}

	if req.Temperature > 0 {
		t := req.Temperature
		apiReq.Temperature = &t
	}
	if req.MaxTokens > 0 {
		m := req.MaxTokens
		apiReq.MaxTokens = &m
	}
	if req.TopP > 0 {
		tp := req.TopP
		apiReq.TopP = &tp
	}
	if len(req.Stop) > 0 {
		apiReq.Stop = req.Stop
	}

	// 传递 tools 配置
	if len(req.Tools) > 0 {
		apiReq.Tools = make([]apiToolDef, len(req.Tools))
		for i, t := range req.Tools {
			apiReq.Tools[i] = apiToolDef{
				Type: t.Type,
				Function: apiToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
	}
	if req.ToolChoice != nil {
		apiReq.ToolChoice = req.ToolChoice
	}

	return apiReq
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}
}
