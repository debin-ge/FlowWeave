package code

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/runtime"
)

type unitSuccessFunction struct{}

func (f *unitSuccessFunction) Name() string { return "test.code.unit.success.v1" }
func (f *unitSuccessFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	in, _ := input["input"].(string)
	return map[string]interface{}{
		"result": strings.ToUpper(in),
	}, nil
}

type unitExtraOutputFunction struct{}

func (f *unitExtraOutputFunction) Name() string { return "test.code.unit.extra.v1" }
func (f *unitExtraOutputFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": "ok",
		"extra":  "not_allowed",
	}, nil
}

type unitSlowFunction struct{}

func (f *unitSlowFunction) Name() string { return "test.code.unit.slow.v1" }
func (f *unitSlowFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(200 * time.Millisecond):
		return map[string]interface{}{"result": "late"}, nil
	}
}

func init() {
	MustRegisterFunction(&unitSuccessFunction{})
	MustRegisterFunction(&unitExtraOutputFunction{})
	MustRegisterFunction(&unitSlowFunction{})
}

func TestNewCodeNodeValidationMissingFunctionRef(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "invalid",
		"inputs": [{"name":"input","type":"string","required":true,"value_selector":["start_1","input"]}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	_, err := NewCodeNode("code_1", raw)
	if err == nil {
		t.Fatal("expected error for missing function_ref")
	}
	if !strings.Contains(err.Error(), string(CodeNodeInvalidConfig)) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestCodeNodeRunSuccess(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "success",
		"function_ref": "test.code.unit.success.v1",
		"inputs": [{"name":"input","type":"string","required":true,"value_selector":["start_1","input"]}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	n, err := NewCodeNode("code_1", raw)
	if err != nil {
		t.Fatalf("NewCodeNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"input": "hello"})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)

	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	succeeded := false
	for _, evt := range events {
		if evt.Type == event.EventTypeNodeRunSucceeded {
			succeeded = true
			if evt.Outputs["result"] != "HELLO" {
				t.Fatalf("unexpected result: %v", evt.Outputs["result"])
			}
		}
	}
	if !succeeded {
		t.Fatal("expected node_run_succeeded event")
	}
}

func TestCodeNodeFunctionNotFound(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "fn-not-found",
		"function_ref": "test.code.unit.missing.v1",
		"inputs": [{"name":"input","type":"string","required":false,"default":"x"}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	n, err := NewCodeNode("code_1", raw)
	if err != nil {
		t.Fatalf("NewCodeNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)
	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	assertHasFailedCode(t, events, CodeNodeFunctionNotFound)
}

func TestCodeNodeInputTypeMismatch(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "input-type-mismatch",
		"function_ref": "test.code.unit.success.v1",
		"inputs": [{"name":"input","type":"number","required":true,"value_selector":["start_1","input"]}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	n, err := NewCodeNode("code_1", raw)
	if err != nil {
		t.Fatalf("NewCodeNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"input": "not-a-number"})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)
	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	assertHasFailedCode(t, events, CodeNodeInputTypeMismatch)
}

func TestCodeNodeOutputSchemaViolation(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "output-schema",
		"function_ref": "test.code.unit.extra.v1",
		"inputs": [{"name":"input","type":"string","required":false,"default":"x"}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	n, err := NewCodeNode("code_1", raw)
	if err != nil {
		t.Fatalf("NewCodeNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)
	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	assertHasFailedCode(t, events, CodeNodeOutputSchemaViolation)
}

func TestCodeNodeExecTimeout(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "func",
		"title": "timeout",
		"function_ref": "test.code.unit.slow.v1",
		"timeout_ms": 50,
		"inputs": [{"name":"input","type":"string","required":false,"default":"x"}],
		"outputs": [{"name":"result","type":"string","required":true}]
	}`)

	n, err := NewCodeNode("code_1", raw)
	if err != nil {
		t.Fatalf("NewCodeNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)
	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	assertHasFailedCode(t, events, CodeNodeExecTimeout)
}

func runNode(n node.Node, ctx context.Context) ([]event.NodeEvent, error) {
	ch, err := n.Run(ctx)
	if err != nil {
		return nil, err
	}
	var eventsOut []event.NodeEvent
	for evt := range ch {
		eventsOut = append(eventsOut, evt)
	}
	return eventsOut, nil
}

func assertHasFailedCode(t *testing.T, events []event.NodeEvent, code ErrorCode) {
	t.Helper()
	for _, evt := range events {
		if evt.Type == event.EventTypeNodeRunFailed {
			if strings.Contains(evt.Error, string(code)) {
				return
			}
		}
	}
	t.Fatalf("expected failure code %s, events=%v", code, events)
}
