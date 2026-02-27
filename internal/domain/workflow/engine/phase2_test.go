package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowweave/internal/app/workflow"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/node/code"
	"flowweave/internal/provider"
	providerPkg "flowweave/internal/provider"
)

// mockLLMProvider 用于测试的 Mock LLM Provider
type mockLLMProvider struct{}

func (m *mockLLMProvider) Name() string { return "mock" }

func (m *mockLLMProvider) Complete(ctx context.Context, req *providerPkg.CompletionRequest) (*providerPkg.CompletionResponse, error) {
	// 将所有消息内容拼接作为响应
	content := "Mock response for: "
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			content += msg.Content
		}
	}
	return &providerPkg.CompletionResponse{
		Content:      content,
		Model:        req.Model,
		FinishReason: "stop",
		Usage:        providerPkg.Usage{TotalTokens: 42},
	}, nil
}

func (m *mockLLMProvider) StreamComplete(ctx context.Context, req *providerPkg.CompletionRequest) (<-chan providerPkg.CompletionChunk, <-chan error) {
	chunkCh := make(chan providerPkg.CompletionChunk, 8)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		words := []string{"Hello", " from", " mock", " LLM", "!"}
		for _, word := range words {
			chunkCh <- providerPkg.CompletionChunk{Delta: word}
		}
		chunkCh <- providerPkg.CompletionChunk{FinishReason: "stop"}
	}()

	return chunkCh, errCh
}

type mockTransformFunction struct{}

func (m *mockTransformFunction) Name() string { return "test.code.transform.v1" }

func (m *mockTransformFunction) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	inputText, _ := inputs["input_text"].(string)
	return map[string]interface{}{
		"result": "PROCESSED:" + strings.ToUpper(inputText),
	}, nil
}

func init() {
	// 注册 Mock Provider
	provider.RegisterProvider(&mockLLMProvider{})
	code.MustRegisterFunction(&mockTransformFunction{})
}

// TestLLMNode 测试 LLM 节点
func TestLLMNode(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "question", "label": "Question", "type": "string", "required": true}
					]
				}
			},
			{
				"id": "llm_1",
				"data": {
					"type": "llm",
					"title": "Ask LLM",
					"model": {
						"provider": "mock",
						"name": "test-model",
						"mode": "chat",
						"temperature": 0.7,
						"max_tokens": 1000
					},
					"prompts": [
						{"role": "system", "text": "You are a helpful assistant."},
						{"role": "user", "text": "{{#start_1.question#}}"}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "answer", "value_selector": ["llm_1", "text"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "llm_1"},
			{"source": "llm_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)
	inputs := map[string]interface{}{
		"question": "What is Go?",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runResult, err := runner.RunSync(ctx, []byte(dsl), inputs, nil)
	if err != nil {
		t.Fatalf("LLM workflow failed: %v", err)
	}

	answer, ok := runResult.Outputs["answer"]
	if !ok {
		t.Fatalf("expected 'answer' in outputs, got: %v", runResult.Outputs)
	}

	answerStr, ok := answer.(string)
	if !ok || answerStr == "" {
		t.Fatalf("expected non-empty string answer, got: %v", answer)
	}

	t.Logf("✅ LLM node test passed, answer: %s", answerStr)
}

// TestCodeNode 测试 Code 节点
func TestCodeNode(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "input_text", "label": "Input", "type": "string", "required": true}
					]
				}
			},
				{
						"id": "code_1",
						"data": {
							"type": "func",
							"title": "Transform",
							"function_ref": "test.code.transform.v1",
						"inputs": [
							{
								"name": "input_text",
								"type": "string",
								"required": true,
								"value_selector": ["start_1", "input_text"]
							}
						],
						"outputs": [
							{"name": "result", "type": "string", "required": true}
						]
					}
				},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "result", "value_selector": ["code_1", "result"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "code_1"},
			{"source": "code_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)
	inputs := map[string]interface{}{
		"input_text": "hello world",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runResult, err := runner.RunSync(ctx, []byte(dsl), inputs, nil)
	if err != nil {
		t.Fatalf("Code workflow failed: %v", err)
	}

	result, ok := runResult.Outputs["result"]
	if !ok {
		t.Fatalf("expected 'result' in outputs, got: %v", runResult.Outputs)
	}

	t.Logf("✅ Code node test passed, result: %v", result)
}

// TestHTTPNode 测试 HTTP 请求节点
func TestHTTPNode(t *testing.T) {
	// 启动测试 HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"message": "hello from test server",
			"method":  r.Method,
			"path":    r.URL.Path,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {"type": "start", "title": "Start", "variables": []}
			},
			{
				"id": "http_1",
				"data": {
					"type": "http-request",
					"title": "Call API",
					"method": "GET",
					"url": "` + server.URL + `/api/test",
					"headers": {"Accept": "application/json"},
					"timeout": 10
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "status", "value_selector": ["http_1", "status_code"]},
						{"variable": "body", "value_selector": ["http_1", "body"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "http_1"},
			{"source": "http_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runResult, err := runner.RunSync(ctx, []byte(dsl), nil, nil)
	if err != nil {
		t.Fatalf("HTTP workflow failed: %v", err)
	}

	status, ok := runResult.Outputs["status"]
	if !ok {
		t.Fatalf("expected 'status' in outputs, got: %v", runResult.Outputs)
	}

	// status_code 是 int，JSON 解码后可能是 float64
	statusCode := 0
	switch v := status.(type) {
	case float64:
		statusCode = int(v)
	case int:
		statusCode = v
	}

	if statusCode != 200 {
		t.Errorf("expected status 200, got %d", statusCode)
	}

	t.Logf("✅ HTTP node test passed, status: %v, body: %v", status, runResult.Outputs["body"])
}

// TestTemplateNode 测试模板转换节点
func TestTemplateNode(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "name", "label": "Name", "type": "string", "required": true},
						{"variable": "age", "label": "Age", "type": "number", "required": true}
					]
				}
			},
			{
				"id": "template_1",
				"data": {
					"type": "template-transform",
					"title": "Format Profile",
					"template": "Name: {{ name }}, Age: {{ age }}",
					"variables": [
						{"variable": "name", "value_selector": ["start_1", "name"]},
						{"variable": "age", "value_selector": ["start_1", "age"]}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "profile", "value_selector": ["template_1", "output"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "template_1"},
			{"source": "template_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)
	inputs := map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runResult, err := runner.RunSync(ctx, []byte(dsl), inputs, nil)
	if err != nil {
		t.Fatalf("Template workflow failed: %v", err)
	}

	profile, ok := runResult.Outputs["profile"]
	if !ok {
		t.Fatalf("expected 'profile' in outputs, got: %v", runResult.Outputs)
	}

	expected := "Name: Alice, Age: 30"
	if profile != expected {
		t.Errorf("expected '%s', got '%v'", expected, profile)
	}

	t.Logf("✅ Template node test passed, output: %v", profile)
}

// TestComplexWorkflow 测试复杂的多节点组合工作流
func TestComplexWorkflow(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "user_query", "label": "Query", "type": "string", "required": true}
					]
				}
			},
			{
				"id": "template_1",
				"data": {
					"type": "template-transform",
					"title": "Format Query",
					"template": "User asked: {{ query }}",
					"variables": [
						{"variable": "query", "value_selector": ["start_1", "user_query"]}
					]
				}
			},
			{
				"id": "ifelse_1",
				"data": {
					"type": "if-else",
					"title": "Check Query Length",
					"conditions": [
						{
							"id": "long_query",
							"logical_operator": "and",
							"conditions": [
								{
									"variable_selector": ["start_1", "user_query"],
									"comparison_operator": "is-not-empty",
									"value": ""
								}
							]
						}
					]
				}
			},
			{
				"id": "llm_1",
				"data": {
					"type": "llm",
					"title": "LLM Response",
					"model": {
						"provider": "mock",
						"name": "test-model",
						"mode": "chat",
						"temperature": 0.7
					},
					"prompts": [
						{"role": "user", "text": "{{#template_1.output#}}"}
					]
				}
			},
			{
				"id": "answer_empty",
				"data": {
					"type": "answer",
					"title": "Empty Query",
					"answer": "Please provide a valid query."
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "response", "value_selector": ["llm_1", "text"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "template_1"},
			{"source": "template_1", "target": "ifelse_1"},
			{"source": "ifelse_1", "target": "llm_1", "sourceHandle": "long_query"},
			{"source": "ifelse_1", "target": "answer_empty", "sourceHandle": "false"},
			{"source": "llm_1", "target": "end_1"},
			{"source": "answer_empty", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	eventCh, err := runner.RunFromDSL(ctx, []byte(dsl), map[string]interface{}{
		"user_query": "What is Go programming?",
	}, nil)
	if err != nil {
		t.Fatalf("Complex workflow failed: %v", err)
	}

	var events []event.GraphEvent
	for evt := range eventCh {
		events = append(events, evt)
	}

	// 验证事件流完整性
	hasStarted := false
	hasSucceeded := false
	hasStreamChunk := false
	for _, evt := range events {
		if evt.Type == event.EventTypeGraphRunStarted {
			hasStarted = true
		}
		if evt.Type == event.EventTypeGraphRunSucceeded {
			hasSucceeded = true
		}
		if evt.Type == event.EventTypeNodeStreamChunk {
			hasStreamChunk = true
		}
	}

	if !hasStarted {
		t.Error("missing graph_run_started")
	}
	if !hasSucceeded {
		t.Error("missing graph_run_succeeded")
	}
	if !hasStreamChunk {
		t.Error("missing stream chunks from LLM node")
	}

	t.Logf("✅ Complex workflow test passed with %d events", len(events))
}
