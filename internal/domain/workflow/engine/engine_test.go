package engine_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"flowweave/internal/app/workflow"
	"flowweave/internal/domain/workflow/event"
)

// TestSimpleStartToEnd 测试最简单的 Start -> End 工作流
func TestSimpleStartToEnd(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "name", "label": "Name", "type": "string", "required": true}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{
							"variable": "result",
							"value_selector": ["start_1", "name"]
						}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)
	inputs := map[string]interface{}{
		"name": "Hello World",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runResult, err := runner.RunSync(ctx, []byte(dsl), inputs, nil)
	if err != nil {
		t.Fatalf("workflow execution failed: %v", err)
	}

	if runResult == nil || runResult.Outputs == nil {
		t.Fatal("expected outputs, got nil")
	}

	result, ok := runResult.Outputs["result"]
	if !ok {
		t.Fatalf("expected 'result' in outputs, got: %v", runResult.Outputs)
	}

	if result != "Hello World" {
		t.Errorf("expected result='Hello World', got: %v", result)
	}

	t.Logf("✅ Start -> End workflow succeeded with outputs: %v", runResult.Outputs)
}

// TestIfElseBranching 测试 IF/ELSE 条件分支
func TestIfElseBranching(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "age", "label": "Age", "type": "number", "required": true}
					]
				}
			},
			{
				"id": "ifelse_1",
				"data": {
					"type": "if-else",
					"title": "Check Age",
					"conditions": [
						{
							"id": "adult",
							"logical_operator": "and",
							"conditions": [
								{
									"variable_selector": ["start_1", "age"],
									"comparison_operator": "gte",
									"value": "18"
								}
							]
						}
					]
				}
			},
			{
				"id": "answer_adult",
				"data": {
					"type": "answer",
					"title": "Adult Answer",
					"answer": "You are an adult"
				}
			},
			{
				"id": "answer_minor",
				"data": {
					"type": "answer",
					"title": "Minor Answer",
					"answer": "You are a minor"
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": []
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "ifelse_1"},
			{"source": "ifelse_1", "target": "answer_adult", "sourceHandle": "adult"},
			{"source": "ifelse_1", "target": "answer_minor", "sourceHandle": "false"},
			{"source": "answer_adult", "target": "end_1"},
			{"source": "answer_minor", "target": "end_1"}
		]
	}`

	tests := []struct {
		name     string
		age      float64
		expected string
	}{
		{"adult_path", 25, "adult"},
		{"minor_path", 15, "minor"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := workflow.NewWorkflowRunner(nil, nil)
			inputs := map[string]interface{}{
				"age": tc.age,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			eventCh, err := runner.RunFromDSL(ctx, []byte(dsl), inputs, nil)
			if err != nil {
				t.Fatalf("workflow execution failed: %v", err)
			}

			var events []event.GraphEvent
			for evt := range eventCh {
				events = append(events, evt)
			}

			// 验证收到了开始和结束事件
			hasStarted := false
			hasSucceeded := false
			for _, evt := range events {
				if evt.Type == event.EventTypeGraphRunStarted {
					hasStarted = true
				}
				if evt.Type == event.EventTypeGraphRunSucceeded {
					hasSucceeded = true
				}
			}

			if !hasStarted {
				t.Error("expected graph_run_started event")
			}
			if !hasSucceeded {
				t.Error("expected graph_run_succeeded event")
			}

			t.Logf("✅ IF/ELSE %s path completed with %d events", tc.expected, len(events))
		})
	}
}

// TestWorkflowEventStream 测试事件流
func TestWorkflowEventStream(t *testing.T) {
	dsl := `{
		"nodes": [
			{
				"id": "start_1",
				"data": {"type": "start", "title": "Start", "variables": []}
			},
			{
				"id": "answer_1",
				"data": {
					"type": "answer",
					"title": "Say Hello",
					"answer": "Hello from workflow!"
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "message", "value_selector": ["answer_1", "answer"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "answer_1"},
			{"source": "answer_1", "target": "end_1"}
		]
	}`

	runner := workflow.NewWorkflowRunner(nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	eventCh, err := runner.RunFromDSL(ctx, []byte(dsl), nil, nil)
	if err != nil {
		t.Fatalf("workflow execution failed: %v", err)
	}

	var events []event.GraphEvent
	for evt := range eventCh {
		events = append(events, evt)
		b, _ := json.Marshal(evt)
		t.Logf("Event: %s", string(b))
	}

	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}

	// 第一个事件应该是开始
	if events[0].Type != event.EventTypeGraphRunStarted {
		t.Errorf("expected first event to be graph_run_started, got %s", events[0].Type)
	}

	// 最后一个事件应该是成功
	lastEvt := events[len(events)-1]
	if lastEvt.Type != event.EventTypeGraphRunSucceeded {
		t.Errorf("expected last event to be graph_run_succeeded, got %s", lastEvt.Type)
	}

	t.Logf("✅ Event stream test passed with %d events", len(events))
}
