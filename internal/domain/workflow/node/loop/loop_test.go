package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/node/code"
	"flowweave/internal/domain/workflow/runtime"
)

type loopStepFunction struct{}

func (f *loopStepFunction) Name() string { return "test.loop.step.v1" }
func (f *loopStepFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	text, _ := input["text"].(string)
	round := toInt(input["round"])
	nextText := fmt.Sprintf("%s:r%d", text, round+1)
	return map[string]interface{}{
		"next_text": nextText,
		"continue":  round < 1,
	}, nil
}

func init() {
	code.MustRegisterFunction(&loopStepFunction{})
}

func TestNewLoopNodeValidationInvalidMode(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"loop",
		"title":"invalid",
		"mode":"for",
		"max_rounds":1,
		"state_init":[{"name":"current_text","default":"x","required":true}],
		"subgraph":{"start":"loop_start","nodes":[{"id":"loop_start","data":{"type":"loop-start","title":"s"}}],"edges":[],"continue_selector":["loop_start","x"]},
		"state_update":[{"name":"current_text","op":"inc","value":1}],
		"continue_condition":{"logical_operator":"and","conditions":[]},
		"outputs":[{"name":"final_text","from":"loop.state.current_text"}]
	}`)

	_, err := NewLoopNode("loop_1", raw)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), LoopInvalidMode) {
		t.Fatalf("expected %s, got %v", LoopInvalidMode, err)
	}
}

func TestLoopNodeRunSuccess(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"loop",
		"title":"loop-run",
		"mode":"while",
		"max_rounds":3,
		"state_init":[
			{"name":"current_text","value_selector":["start_1","text"],"required":true},
			{"name":"round","default":0,"required":true}
		],
		"subgraph":{
			"start":"loop_start",
			"nodes":[
				{"id":"loop_start","data":{"type":"loop-start","title":"Start"}},
				{
					"id":"step_1",
					"data":{
						"type":"func",
						"title":"Step",
						"function_ref":"test.loop.step.v1",
						"inputs":[
							{"name":"text","type":"string","required":true,"value_selector":["loop_1","current_text"]},
							{"name":"round","type":"number","required":true,"value_selector":["loop_1","round"]}
						],
						"outputs":[
							{"name":"next_text","type":"string","required":true},
							{"name":"continue","type":"boolean","required":true}
						]
					}
				}
			],
			"edges":[{"source":"loop_start","target":"step_1"}],
			"continue_selector":["step_1","continue"]
		},
		"state_update":[
			{"name":"current_text","op":"assign","value_selector":["step_1","next_text"],"required":true},
			{"name":"round","op":"inc","value":1}
		],
		"continue_condition":{
			"logical_operator":"and",
			"conditions":[
				{"variable_selector":["loop_internal","continue_raw"],"comparison_operator":"is","value":"true"},
				{"variable_selector":["loop_internal","round_lt_max"],"comparison_operator":"is","value":"true"}
			]
		},
		"outputs":[
			{"name":"final_text","from":"loop.state.current_text","required":true},
			{"name":"rounds","from":"loop.meta.rounds","required":true}
		]
	}`)

	n, err := NewLoopNode("loop_1", raw)
	if err != nil {
		t.Fatalf("NewLoopNode failed: %v", err)
	}

	vp := runtime.NewVariablePool()
	vp.SetNodeOutputs("start_1", map[string]interface{}{"text": "seed"})
	ctx := context.WithValue(context.Background(), node.ContextKeyVariablePool, vp)

	events, err := runNode(n, ctx)
	if err != nil {
		t.Fatalf("node run failed: %v", err)
	}

	for _, evt := range events {
		if evt.Type != event.EventTypeNodeRunSucceeded {
			continue
		}
		if evt.Outputs["final_text"] != "seed:r1:r2" {
			t.Fatalf("unexpected final_text: %v", evt.Outputs["final_text"])
		}
		rounds := toInt(evt.Outputs["rounds"])
		if rounds != 2 {
			t.Fatalf("expected rounds=2, got %v", evt.Outputs["rounds"])
		}
		return
	}
	t.Fatalf("expected node_run_succeeded event, events=%v", events)
}

func runNode(n node.Node, ctx context.Context) ([]event.NodeEvent, error) {
	ch, err := n.Run(ctx)
	if err != nil {
		return nil, err
	}
	var out []event.NodeEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out, nil
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
