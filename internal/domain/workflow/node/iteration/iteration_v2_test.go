package iteration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/node/code"
	"flowweave/internal/domain/workflow/runtime"
)

type testIterationIdentityFunction struct{}

func (f *testIterationIdentityFunction) Name() string { return "test.iteration.identity.v1" }
func (f *testIterationIdentityFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"result": input["item"]}, nil
}

func init() {
	code.MustRegisterFunction(&testIterationIdentityFunction{})
}

func TestValidateIterationNodeDataSelectorLength(t *testing.T) {
	d := &IterationNodeData{
		Type:  "iteration",
		Mode:  "map",
		Input: IterationInput{ValueSelector: []string{"start_1"}},
		Subgraph: IterationSubgraph{
			Start:          "iter_start",
			Nodes:          []types.NodeConfig{{ID: "iter_start"}},
			Edges:          []types.EdgeConfig{},
			ResultSelector: []string{"func_1", "result"},
		},
		Concurrency: &IterationConcurrency{MaxConcurrency: 1, Order: orderInput},
		Aggregate:   IterationAggregate{Strategy: aggregateStrategyCollect},
		Outputs:     []IterationOutput{{Name: "results", From: "aggregate.result"}},
	}

	err := ValidateIterationNodeData(d)
	if err == nil || !strings.Contains(err.Error(), IterationInputSelectorInvalid) {
		t.Fatalf("expected %s, got %v", IterationInputSelectorInvalid, err)
	}
}

func TestIterationNodeCollectSuccess(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "iteration",
		"title": "collect",
		"mode": "map",
		"input": {"value_selector": ["start_1", "items"]},
		"subgraph": {
			"start": "iter_start",
			"nodes": [
				{"id":"iter_start","data":{"type":"iteration-start","title":"start"}},
				{"id":"func_1","data":{
					"type":"func",
					"title":"identity",
					"function_ref":"test.iteration.identity.v1",
					"inputs":[{"name":"item","type":"string","required":true,"value_selector":["iter_1","item"]}],
					"outputs":[{"name":"result","type":"string","required":true}]
				}}
			],
			"edges": [{"source":"iter_start","target":"func_1"}],
			"result_selector": ["func_1", "result"]
		},
		"concurrency": {"max_concurrency": 2, "order": "input-order"},
		"aggregate": {"strategy": "collect"},
		"outputs": [
			{"name": "results", "from": "aggregate.result"},
			{"name": "meta", "from": "aggregate.meta"}
		]
	}`)

	n, err := NewIterationNode("iter_1", raw)
	if err != nil {
		t.Fatalf("NewIterationNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"items": []interface{}{"a", "b", "c"}})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)

	events, err := runNodeEvents(n, ctx)
	if err != nil {
		t.Fatalf("run iteration failed: %v", err)
	}
	succeeded := findSuccessEvent(t, events)
	results, ok := succeeded.Outputs["results"].([]interface{})
	if !ok || len(results) != 3 {
		t.Fatalf("expected 3 collect results, got %v", succeeded.Outputs["results"])
	}
	first, ok := results[0].(map[string]interface{})
	if !ok || first["result"] != "a" {
		t.Fatalf("unexpected first result: %v", results[0])
	}
	second, ok := results[1].(map[string]interface{})
	if !ok || second["result"] != "b" {
		t.Fatalf("unexpected second result: %v", results[1])
	}
	third, ok := results[2].(map[string]interface{})
	if !ok || third["result"] != "c" {
		t.Fatalf("unexpected third result: %v", results[2])
	}
	meta, ok := succeeded.Outputs["meta"].(map[string]interface{})
	if !ok || meta["success"] != 3 {
		t.Fatalf("unexpected meta: %v", succeeded.Outputs["meta"])
	}
}

func TestIterationNodeContinueOnItemError(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "iteration",
		"title": "continue",
		"mode": "map",
		"input": {"value_selector": ["start_1", "items"]},
		"subgraph": {
			"start": "iter_start",
			"nodes": [
				{"id":"iter_start","data":{"type":"iteration-start","title":"start"}},
				{"id":"func_1","data":{
					"type":"func",
					"title":"missing",
					"function_ref":"test.iteration.missing.v1",
					"inputs":[{"name":"item","type":"string","required":true,"value_selector":["iter_1","item"]}],
					"outputs":[{"name":"result","type":"string","required":true}]
				}}
			],
			"edges": [{"source":"iter_start","target":"func_1"}],
			"result_selector": ["func_1", "result"]
		},
		"aggregate": {"strategy": "collect"},
		"on_item_error": "continue",
		"outputs": [
			{"name": "results", "from": "aggregate.result"},
			{"name": "errors", "from": "aggregate.errors"},
			{"name": "meta", "from": "aggregate.meta"}
		]
	}`)

	n, err := NewIterationNode("iter_1", raw)
	if err != nil {
		t.Fatalf("NewIterationNode failed: %v", err)
	}
	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"items": []interface{}{"a", "b"}})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)

	events, err := runNodeEvents(n, ctx)
	if err != nil {
		t.Fatalf("run iteration failed: %v", err)
	}
	succeeded := findSuccessEvent(t, events)
	results, ok := succeeded.Outputs["results"].([]interface{})
	if !ok || len(results) != 0 {
		t.Fatalf("expected empty results on all-failed continue, got %v", succeeded.Outputs["results"])
	}
	errs, ok := succeeded.Outputs["errors"].([]map[string]interface{})
	if !ok || len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %v", succeeded.Outputs["errors"])
	}
	meta, ok := succeeded.Outputs["meta"].(map[string]interface{})
	if !ok || meta["failed"] != 2 {
		t.Fatalf("unexpected meta: %v", succeeded.Outputs["meta"])
	}
}

func TestIterationNodeFailFastOnItemError(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "iteration",
		"title": "fail-fast",
		"mode": "map",
		"input": {"value_selector": ["start_1", "items"]},
		"subgraph": {
			"start": "iter_start",
			"nodes": [
				{"id":"iter_start","data":{"type":"iteration-start","title":"start"}},
				{"id":"func_1","data":{
					"type":"func",
					"title":"missing",
					"function_ref":"test.iteration.missing.v1",
					"inputs":[{"name":"item","type":"string","required":true,"value_selector":["iter_1","item"]}],
					"outputs":[{"name":"result","type":"string","required":true}]
				}}
			],
			"edges": [{"source":"iter_start","target":"func_1"}],
			"result_selector": ["func_1", "result"]
		},
		"aggregate": {"strategy": "collect"},
		"on_item_error": "fail-fast",
		"outputs": [
			{"name": "results", "from": "aggregate.result"}
		]
	}`)

	n, err := NewIterationNode("iter_1", raw)
	if err != nil {
		t.Fatalf("NewIterationNode failed: %v", err)
	}
	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"items": []interface{}{"a", "b"}})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)

	events, err := runNodeEvents(n, ctx)
	if err != nil {
		t.Fatalf("run iteration failed: %v", err)
	}
	failed := findFailedEvent(t, events)
	if !strings.Contains(failed.Error, IterationItemExecFailed) {
		t.Fatalf("expected %s, got %s", IterationItemExecFailed, failed.Error)
	}
}

func TestIterationNodeMergeRejected(t *testing.T) {
	mergeRaw := json.RawMessage(`{
		"type": "iteration",
		"title": "merge",
		"mode": "map",
		"input": {"value_selector": ["start_1", "items"]},
		"subgraph": {
			"start": "iter_start",
			"nodes": [
				{"id":"iter_start","data":{"type":"iteration-start","title":"start"}},
				{"id":"func_1","data":{
					"type":"func",
					"title":"identity",
					"function_ref":"test.iteration.identity.v1",
					"inputs":[{"name":"item","type":"object","required":true,"value_selector":["iter_1","item"]}],
					"outputs":[{"name":"result","type":"object","required":true}]
				}}
			],
			"edges": [{"source":"iter_start","target":"func_1"}],
			"result_selector": ["func_1", "result"]
		},
		"aggregate": {"strategy": "merge", "merge": {"conflict": "last-write-wins"}},
		"outputs": [{"name": "merged", "from": "aggregate.result"}]
	}`)

	_, err := NewIterationNode("iter_1", mergeRaw)
	if err == nil || !strings.Contains(err.Error(), IterationAggregateInvalid) {
		t.Fatalf("expected %s for merge strategy, got %v", IterationAggregateInvalid, err)
	}
}

func TestIterationNodeReduceInline(t *testing.T) {
	reduceRaw := json.RawMessage(`{
		"type": "iteration",
		"title": "reduce",
		"mode": "map",
		"input": {"value_selector": ["start_1", "items"]},
		"subgraph": {
			"start": "iter_start",
			"nodes": [
				{"id":"iter_start","data":{"type":"iteration-start","title":"start"}},
				{"id":"func_1","data":{
					"type":"func",
					"title":"identity",
					"function_ref":"test.iteration.identity.v1",
					"inputs":[{"name":"item","type":"number","required":true,"value_selector":["iter_1","item"]}],
					"outputs":[{"name":"result","type":"number","required":true}]
				}}
			],
			"edges": [{"source":"iter_start","target":"func_1"}],
			"result_selector": ["func_1", "result"]
		},
		"aggregate": {"strategy": "reduce", "reduce": {"acc_init": 0}},
		"outputs": [{"name": "sum", "from": "aggregate.result"}]
	}`)

	reduceNode, err := NewIterationNode("iter_1", reduceRaw)
	if err != nil {
		t.Fatalf("NewIterationNode reduce failed: %v", err)
	}
	reduceVP := runtime.NewVariablePool()
	reduceVP.SetNodeOutputs("start_1", map[string]interface{}{"items": []interface{}{1, 2, 3}})
	reduceCtx := context.WithValue(context.Background(), node.ContextKeyVariablePool, reduceVP)
	reduceEvents, err := runNodeEvents(reduceNode, reduceCtx)
	if err != nil {
		t.Fatalf("run reduce failed: %v", err)
	}
	reduceSuccess := findSuccessEvent(t, reduceEvents)
	sum, err := toFloat(reduceSuccess.Outputs["sum"])
	if err != nil || sum != 6 {
		t.Fatalf("expected sum=6, got %v (err=%v)", reduceSuccess.Outputs["sum"], err)
	}
}

func runNodeEvents(n node.Node, ctx context.Context) ([]event.NodeEvent, error) {
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

func findSuccessEvent(t *testing.T, events []event.NodeEvent) event.NodeEvent {
	t.Helper()
	for _, evt := range events {
		if evt.Type == event.EventTypeNodeRunSucceeded {
			return evt
		}
	}
	t.Fatalf("expected success event, got %v", events)
	return event.NodeEvent{}
}

func findFailedEvent(t *testing.T, events []event.NodeEvent) event.NodeEvent {
	t.Helper()
	for _, evt := range events {
		if evt.Type == event.EventTypeNodeRunFailed {
			return evt
		}
	}
	t.Fatalf("expected failed event, got %v", events)
	return event.NodeEvent{}
}

func toFloat(v interface{}) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("not numeric: %T", v)
	}
}
