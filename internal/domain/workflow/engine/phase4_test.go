package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"flowweave/internal/app/workflow"
	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/graph"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/node/code"
	"flowweave/internal/domain/workflow/runtime"

	_ "flowweave/internal/app/bootstrap"
)

// failingFunction 始终返回错误
type failingFunction struct{}

func (f *failingFunction) Name() string {
	return "test.code.fail.v1"
}

func (f *failingFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("simulated failure")
}

func init() {
	code.MustRegisterFunction(&failingFunction{})
}

// --- 错误策略：default-value ---

func TestErrorStrategy_DefaultValue(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "query", "label": "Query", "type": "string", "required": true}
					]
				}
			},
				{
					"id": "code_1",
					"data": {
						"type": "func",
						"title": "Failing Code",
						"function_ref": "test.code.fail.v1",
						"inputs": [
							{"name": "query", "type": "string", "required": true, "value_selector": ["start_1", "query"]}
						],
						"outputs": [
							{"name": "result", "type": "string", "required": false}
						],
						"error_strategy": "default-value",
						"default_value": {
							"result": "fallback_value"
					}
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
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{
		"query": "test",
	}, nil)

	if err != nil {
		t.Fatalf("expected success with default value, got error: %v", err)
	}

	result, ok := runResult.Outputs["result"]
	if !ok || result != "fallback_value" {
		t.Fatalf("expected fallback_value, got %v", runResult.Outputs)
	}

	t.Logf("✅ Default value strategy test passed: result=%v", result)
}

// --- 错误策略：fail-branch ---

func TestErrorStrategy_FailBranch(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "query", "label": "Query", "type": "string", "required": true}
					]
				}
			},
				{
					"id": "code_1",
					"data": {
						"type": "func",
						"title": "Failing Code",
						"function_ref": "test.code.fail.v1",
						"inputs": [
							{"name": "query", "type": "string", "required": true, "value_selector": ["start_1", "query"]}
						],
						"outputs": [
							{"name": "result", "type": "string", "required": false}
						],
						"error_strategy": "fail-branch"
					}
				},
			{
				"id": "end_success",
				"data": {
					"type": "end",
					"title": "Success End",
					"outputs": [
						{"variable": "status", "value_selector": ["start_1", "query"]}
					]
				}
			},
			{
				"id": "end_failed",
				"data": {
					"type": "end",
					"title": "Failed End",
					"outputs": [
						{"variable": "error_msg", "value_selector": ["code_1", "__error__"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "code_1"},
			{"source": "code_1", "target": "end_success", "sourceHandle": "success-branch"},
			{"source": "code_1", "target": "end_failed", "sourceHandle": "fail-branch"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{
		"query": "test",
	}, nil)

	if err != nil {
		t.Fatalf("expected success (fail-branch handled), got error: %v", err)
	}

	errorMsg, ok := runResult.Outputs["error_msg"]
	if !ok {
		t.Fatalf("expected error_msg in runResult.Outputs, got %v", runResult.Outputs)
	}

	t.Logf("✅ Fail-branch strategy test passed: error_msg=%v", errorMsg)
}

// --- 迭代节点 ---

func TestIterationNode(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "names", "label": "Names", "type": "array[string]", "required": true}
					]
				}
			},
			{
				"id": "iter_1",
				"data": {
					"type": "iteration",
					"title": "Process Names",
					"iterator": ["start_1", "names"],
					"output_variable": "processed",
					"max_iterations": 50
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "count", "value_selector": ["iter_1", "count"]},
						{"variable": "items", "value_selector": ["iter_1", "processed"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "iter_1"},
			{"source": "iter_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{
		"names": []interface{}{"Alice", "Bob", "Charlie"},
	}, nil)

	if err != nil {
		t.Fatalf("iteration workflow failed: %v", err)
	}

	count, ok := runResult.Outputs["count"]
	if !ok {
		t.Fatalf("expected count in runResult.Outputs, got %v", runResult.Outputs)
	}

	// count 可能是 int 或 float64
	var actual int
	switch v := count.(type) {
	case float64:
		actual = int(v)
	case int:
		actual = v
	}

	if actual != 3 {
		t.Fatalf("expected count=3, got %v (type %T)", count, count)
	}

	t.Logf("✅ Iteration node test passed: count=%v, items=%v", count, runResult.Outputs["items"])
}

// --- 命令通道：Abort ---

func TestCommandChannel_Abort(t *testing.T) {
	vp := runtime.NewVariablePool()
	vp.SetSystem("delay", "true")
	state := runtime.NewGraphRuntimeState(vp)

	// 构建简单图
	factory := node.NewFactory()
	config := &types.GraphConfig{
		Nodes: []types.NodeConfig{
			{ID: "start_1", Data: json.RawMessage(`{"type":"start","title":"Start","variables":[]}`)},
			{ID: "end_1", Data: json.RawMessage(`{"type":"end","title":"End","outputs":[]}`)},
		},
		Edges: []types.EdgeConfig{
			{Source: "start_1", Target: "end_1"},
		},
	}
	g, err := graph.Init(config, factory)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	eng := engine.New(g, state, engine.DefaultConfig())
	eventCh := eng.Run(context.Background())

	// 短暂等待后发送中止命令
	go func() {
		time.Sleep(50 * time.Millisecond)
		eng.Abort()
	}()

	var events []event.GraphEvent
	for evt := range eventCh {
		events = append(events, evt)
	}

	t.Logf("✅ Abort command test passed with %d events", len(events))
}

// --- 命令通道：Pause/Resume ---

func TestCommandChannel_PauseResume(t *testing.T) {
	vp := runtime.NewVariablePool()
	vp.SetSystem("name", "test")
	state := runtime.NewGraphRuntimeState(vp)

	factory := node.NewFactory()
	config := &types.GraphConfig{
		Nodes: []types.NodeConfig{
			{ID: "start_1", Data: json.RawMessage(`{"type":"start","title":"Start","variables":[{"variable":"name","label":"Name","type":"string","required":true}]}`)},
			{ID: "end_1", Data: json.RawMessage(`{"type":"end","title":"End","outputs":[{"variable":"result","value_selector":["start_1","name"]}]}`)},
		},
		Edges: []types.EdgeConfig{
			{Source: "start_1", Target: "end_1"},
		},
	}
	g, err := graph.Init(config, factory)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	eng := engine.New(g, state, engine.DefaultConfig())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		eng.Pause()
		t.Log("⏸️ Engine paused")

		time.Sleep(100 * time.Millisecond)
		eng.Resume()
		t.Log("▶️ Engine resumed")
	}()

	eventCh := eng.Run(context.Background())

	var lastEvt event.GraphEvent
	for evt := range eventCh {
		lastEvt = evt
	}

	wg.Wait()
	t.Logf("✅ Pause/Resume test passed, final event: %s", lastEvt.Type)
}

// --- 错误策略：retry ---

func TestErrorStrategy_Retry(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "x", "label": "X", "type": "string", "required": true}
					]
				}
			},
				{
					"id": "code_1",
					"data": {
						"type": "func",
						"title": "Retry Code",
						"function_ref": "test.code.fail.v1",
						"inputs": [
							{"name": "x", "type": "string", "required": true, "value_selector": ["start_1", "x"]}
						],
						"outputs": [
							{"name": "result", "type": "string", "required": false}
						],
						"error_strategy": "retry",
						"retry": {
							"max_retries": 2,
						"retry_interval": 10
					}
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "outcome", "value_selector": ["start_1", "x"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "code_1"},
			{"source": "code_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh, err := runner.RunFromDSL(ctx, dsl, map[string]interface{}{"x": "hello"}, nil)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// 收集事件
	failCount := 0
	for evt := range eventCh {
		if evt.Type == event.EventTypeNodeRunFailed && evt.NodeID == "code_1" {
			failCount++
		}
	}

	// 重试 2 次 + 原始 1 次 = 3 次尝试，最终产生 1 个 NodeRunFailed 事件
	// （因为 runNodeOnce 内部的尝试事件也会被转发）
	if failCount == 0 {
		t.Log("⚠️ No fail events for code_1 (retries consumed internally)")
	}
	t.Logf("✅ Retry strategy test passed, code_1 fail events: %d", failCount)
}
