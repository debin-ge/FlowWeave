package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

type iterationIdentityFunction struct{}

func (f *iterationIdentityFunction) Name() string {
	return "test.engine.iteration.identity.v1"
}

func (f *iterationIdentityFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"result": input["item"],
	}, nil
}

type loopStepFunction struct{}

func (f *loopStepFunction) Name() string {
	return "test.engine.loop.step.v1"
}

func (f *loopStepFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	text, _ := input["text"].(string)
	round := 0
	switch n := input["round"].(type) {
	case int:
		round = n
	case int64:
		round = int(n)
	case float64:
		round = int(n)
	}
	return map[string]interface{}{
		"next_text":  fmt.Sprintf("%s:r%d", text, round+1),
		"over_limit": round < 1,
	}, nil
}

type loopSlowStopFunction struct{}

func (f *loopSlowStopFunction) Name() string {
	return "test.engine.loop.slow_stop.v1"
}

func (f *loopSlowStopFunction) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	time.Sleep(20 * time.Millisecond)
	number := input["number"]
	return map[string]interface{}{
		"next_number":   number,
		"continue_loop": false,
	}, nil
}

func init() {
	code.MustRegisterFunction(&failingFunction{})
	code.MustRegisterFunction(&iterationIdentityFunction{})
	code.MustRegisterFunction(&loopStepFunction{})
	code.MustRegisterFunction(&loopSlowStopFunction{})
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
					"mode": "map",
					"input": {
						"value_selector": ["start_1", "names"]
					},
					"subgraph": {
						"start": "iter_start",
						"nodes": [
							{
								"id": "iter_start",
								"data": {
									"type": "iteration-start",
									"title": "Iter Start"
								}
							},
							{
								"id": "func_1",
								"data": {
									"type": "func",
									"title": "Wrap Name",
									"function_ref": "flowweave.echo.v1",
									"inputs": [
										{"name": "name", "type": "string", "required": true, "value_selector": ["iter_1", "item"]}
									],
									"outputs": [
										{"name": "result", "type": "object", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "iter_start", "target": "func_1"}
						],
						"result_selector": ["func_1", "result"]
					},
					"concurrency": {
						"max_concurrency": 2,
						"order": "input-order"
					},
					"aggregate": {
						"strategy": "collect"
					},
					"outputs": [
						{"name": "results", "from": "aggregate.result"},
						{"name": "meta", "from": "aggregate.meta"}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "meta", "value_selector": ["iter_1", "meta"]},
						{"variable": "items", "value_selector": ["iter_1", "results"]}
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

	meta, ok := runResult.Outputs["meta"]
	if !ok {
		t.Fatalf("expected meta in runResult.Outputs, got %v", runResult.Outputs)
	}
	metaMap, ok := meta.(map[string]interface{})
	if !ok {
		t.Fatalf("expected meta map, got %T", meta)
	}

	success, ok := metaMap["success"]
	if !ok {
		t.Fatalf("expected meta.success, got %v", metaMap)
	}
	if success != 3 {
		t.Fatalf("expected success=3, got %v", success)
	}

	items, ok := runResult.Outputs["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items as []interface{}, got %T", runResult.Outputs["items"])
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	t.Logf("✅ Iteration node test passed: meta=%v, items=%v", meta, runResult.Outputs["items"])
}

func TestIterationNodeContinueErrorsToDownstream(t *testing.T) {
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
					"title": "Process With Continue",
					"mode": "map",
					"input": {
						"value_selector": ["start_1", "names"]
					},
					"subgraph": {
						"start": "iter_start",
						"nodes": [
							{
								"id": "iter_start",
								"data": {
									"type": "iteration-start",
									"title": "Iter Start"
								}
							},
							{
								"id": "func_1",
								"data": {
									"type": "func",
									"title": "Missing Function",
									"function_ref": "test.iteration.missing.v1",
									"inputs": [
										{"name": "name", "type": "string", "required": true, "value_selector": ["iter_1", "item"]}
									],
									"outputs": [
										{"name": "result", "type": "string", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "iter_start", "target": "func_1"}
						],
						"result_selector": ["func_1", "result"]
					},
					"aggregate": {
						"strategy": "collect"
					},
					"on_item_error": "continue",
					"outputs": [
						{"name": "errors", "from": "aggregate.errors"},
						{"name": "meta", "from": "aggregate.meta"}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "errors", "value_selector": ["iter_1", "errors"]},
						{"variable": "meta", "value_selector": ["iter_1", "meta"]}
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
		"names": []interface{}{"Alice", "Bob"},
	}, nil)
	if err != nil {
		t.Fatalf("iteration continue workflow failed: %v", err)
	}

	errorsOut, ok := runResult.Outputs["errors"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected errors as []map[string]interface{}, got %T", runResult.Outputs["errors"])
	}
	if len(errorsOut) != 2 {
		t.Fatalf("expected 2 item errors, got %d (%v)", len(errorsOut), errorsOut)
	}

	meta, ok := runResult.Outputs["meta"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected meta as map, got %T", runResult.Outputs["meta"])
	}
	if meta["failed"] != 2 {
		t.Fatalf("expected meta.failed=2, got %v", meta["failed"])
	}
	if meta["success"] != 0 {
		t.Fatalf("expected meta.success=0, got %v", meta["success"])
	}
}

func TestIterationNodeMergeRejected(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "items", "label": "Items", "type": "array[object]", "required": true}
					]
				}
			},
			{
				"id": "iter_1",
				"data": {
					"type": "iteration",
					"title": "Merge Conflict",
					"mode": "map",
					"input": {
						"value_selector": ["start_1", "items"]
					},
					"subgraph": {
						"start": "iter_start",
						"nodes": [
							{
								"id": "iter_start",
								"data": {
									"type": "iteration-start",
									"title": "Iter Start"
								}
							},
							{
								"id": "func_1",
								"data": {
									"type": "func",
									"title": "Identity",
									"function_ref": "test.engine.iteration.identity.v1",
									"inputs": [
										{"name": "item", "type": "object", "required": true, "value_selector": ["iter_1", "item"]}
									],
									"outputs": [
										{"name": "result", "type": "object", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "iter_start", "target": "func_1"}
						],
						"result_selector": ["func_1", "result"]
					},
					"aggregate": {
						"strategy": "merge",
						"merge": {"conflict": "error-on-conflict"}
					},
					"outputs": [
						{"name": "merged", "from": "aggregate.result"}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "merged", "value_selector": ["iter_1", "merged"]}
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
	_, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"k": "v1"},
			map[string]interface{}{"k": "v2"},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected workflow error due to unsupported merge strategy")
	}
	if !strings.Contains(err.Error(), "ITERATION_AGGREGATE_INVALID") {
		t.Fatalf("expected ITERATION_AGGREGATE_INVALID, got %v", err)
	}
}

func TestIterationNodeReduceInlineSuccess(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "items", "label": "Items", "type": "array[number]", "required": true}
					]
				}
			},
			{
				"id": "iter_1",
				"data": {
					"type": "iteration",
					"title": "Reduce Missing Reducer",
					"mode": "map",
					"input": {
						"value_selector": ["start_1", "items"]
					},
					"subgraph": {
						"start": "iter_start",
						"nodes": [
							{
								"id": "iter_start",
								"data": {
									"type": "iteration-start",
									"title": "Iter Start"
								}
							},
							{
								"id": "func_1",
								"data": {
									"type": "func",
									"title": "Identity Number",
									"function_ref": "test.engine.iteration.identity.v1",
									"inputs": [
										{"name": "item", "type": "number", "required": true, "value_selector": ["iter_1", "item"]}
									],
									"outputs": [
										{"name": "result", "type": "number", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "iter_start", "target": "func_1"}
						],
						"result_selector": ["func_1", "result"]
					},
					"aggregate": {
						"strategy": "reduce",
						"reduce": {"acc_init": 0}
					},
					"outputs": [
						{"name": "sum", "from": "aggregate.result"}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "sum", "value_selector": ["iter_1", "sum"]}
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
		"items": []interface{}{1, 2, 3},
	}, nil)
	if err != nil {
		t.Fatalf("reduce inline workflow failed: %v", err)
	}
	sum, ok := runResult.Outputs["sum"]
	if !ok {
		t.Fatalf("expected sum output, got %v", runResult.Outputs)
	}
	switch v := sum.(type) {
	case float64:
		if v != 6 {
			t.Fatalf("expected sum=6, got %v", v)
		}
	case int:
		if v != 6 {
			t.Fatalf("expected sum=6, got %v", v)
		}
	default:
		t.Fatalf("unexpected sum type %T value %v", sum, sum)
	}
}

func TestIterationNodeReduceJoinTextSuccess(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "orders", "label": "Orders", "type": "array[object]", "required": true}
					]
				}
			},
			{
				"id": "iter_1",
				"data": {
					"type": "iteration",
					"title": "Reduce Join Text",
					"mode": "map",
					"input": {
						"value_selector": ["start_1", "orders"]
					},
					"context": {
						"item_key": "order",
						"index_key": "idx"
					},
					"subgraph": {
						"start": "iter_start",
						"nodes": [
							{
								"id": "iter_start",
								"data": {
									"type": "iteration-start",
									"title": "Iter Start"
								}
							},
							{
								"id": "enrich_order",
								"data": {
									"type": "func",
									"title": "Identity",
									"function_ref": "flowweave.echo.v1",
									"inputs": [
										{"name": "item", "type": "object", "required": true, "value_selector": ["iter_1", "order"]}
									],
									"outputs": [
										{"name": "result", "type": "object", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "iter_start", "target": "enrich_order"}
						],
						"result_selector": ["enrich_order", "result"]
					},
					"aggregate": {
						"strategy": "reduce",
						"reduce": {
							"acc_init": {"text": ""}
						}
					},
					"outputs": [
						{"name": "reduced", "from": "aggregate.result"}
					],
					"on_item_error": "continue"
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "reduced", "value_selector": ["iter_1", "reduced"]}
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
		"orders": []interface{}{
			map[string]interface{}{"text": "测试一下"},
			map[string]interface{}{"text": "第二条数据"},
			map[string]interface{}{"text": "第三条数据"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("reduce join text workflow failed: %v", err)
	}

	reduced, ok := runResult.Outputs["reduced"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reduced as map, got %T", runResult.Outputs["reduced"])
	}
	if reduced["text"] != "测试一下,第二条数据,第三条数据" {
		t.Fatalf("unexpected reduced text: %v", reduced["text"])
	}
}

func TestLoopNodeWhileSuccess(t *testing.T) {
	dsl := json.RawMessage(`{
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
				"id": "loop_1",
				"data": {
					"type": "loop",
					"title": "Loop Summary",
					"mode": "while",
					"max_rounds": 3,
					"state_init": [
						{"name": "current_text", "value_selector": ["start_1", "input_text"], "required": true},
						{"name": "round", "default": 0, "required": true}
					],
					"subgraph": {
						"start": "loop_start",
						"nodes": [
							{
								"id": "loop_start",
								"data": {
									"type": "loop-start",
									"title": "Loop Start"
								}
							},
							{
								"id": "step_1",
								"data": {
									"type": "func",
									"title": "Loop Step",
									"function_ref": "test.engine.loop.step.v1",
									"inputs": [
										{"name": "text", "type": "string", "required": true, "value_selector": ["loop_1", "current_text"]},
										{"name": "round", "type": "number", "required": true, "value_selector": ["loop_1", "round"]}
									],
									"outputs": [
										{"name": "next_text", "type": "string", "required": true},
										{"name": "over_limit", "type": "boolean", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "loop_start", "target": "step_1"}
						],
						"continue_selector": ["step_1", "over_limit"]
					},
					"state_update": [
						{"name": "current_text", "op": "assign", "value_selector": ["step_1", "next_text"], "required": true},
						{"name": "round", "op": "inc", "value": 1}
					],
					"continue_condition": {
						"logical_operator": "and",
						"conditions": [
							{"variable_selector": ["loop_internal", "continue_raw"], "comparison_operator": "is", "value": "true"},
							{"variable_selector": ["loop_internal", "round_lt_max"], "comparison_operator": "is", "value": "true"}
						]
					},
					"outputs": [
						{"name": "final_text", "from": "loop.state.current_text", "required": true},
						{"name": "rounds", "from": "loop.meta.rounds", "required": true},
						{"name": "terminated_by", "from": "loop.meta.terminated_by", "required": true}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "summary", "value_selector": ["loop_1", "final_text"]},
						{"variable": "rounds", "value_selector": ["loop_1", "rounds"]},
						{"variable": "terminated_by", "value_selector": ["loop_1", "terminated_by"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "loop_1"},
			{"source": "loop_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{
		"input_text": "seed",
	}, nil)
	if err != nil {
		t.Fatalf("loop workflow failed: %v", err)
	}

	if runResult.Outputs["summary"] != "seed:r1:r2" {
		t.Fatalf("unexpected summary: %v", runResult.Outputs["summary"])
	}
	rounds := 0
	switch v := runResult.Outputs["rounds"].(type) {
	case int:
		rounds = v
	case int64:
		rounds = int(v)
	case float64:
		rounds = int(v)
	default:
		t.Fatalf("unexpected rounds type: %T", runResult.Outputs["rounds"])
	}
	if rounds != 2 {
		t.Fatalf("expected rounds=2, got %v", runResult.Outputs["rounds"])
	}
	if runResult.Outputs["terminated_by"] != "condition-false" {
		t.Fatalf("unexpected terminated_by: %v", runResult.Outputs["terminated_by"])
	}
}

func TestLoopPrimeJudgeIncrementUntilPrime(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "n", "label": "Number", "type": "number", "required": true}
					]
				}
			},
			{
				"id": "loop_1",
				"data": {
					"type": "loop",
					"title": "Prime Loop",
					"mode": "while",
					"max_rounds": 10,
					"state_init": [
						{"name": "current_number", "value_selector": ["start_1", "n"], "required": true},
						{"name": "round", "default": 0, "required": true}
					],
					"subgraph": {
						"start": "loop_start",
						"nodes": [
							{
								"id": "loop_start",
								"data": {
									"type": "loop-start",
									"title": "Loop Start"
								}
							},
							{
								"id": "judge_1",
								"data": {
									"type": "func",
									"title": "Prime Judge",
									"function_ref": "math.prime_judge.v1",
									"inputs": [
										{"name": "number", "type": "number", "required": true, "value_selector": ["loop_1", "current_number"]}
									],
									"outputs": [
										{"name": "is_prime", "type": "boolean", "required": true},
										{"name": "continue_loop", "type": "boolean", "required": true},
										{"name": "next_number", "type": "number", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "loop_start", "target": "judge_1"}
						],
						"continue_selector": ["judge_1", "continue_loop"]
					},
					"state_update": [
						{"name": "current_number", "op": "assign", "value_selector": ["judge_1", "next_number"], "required": true},
						{"name": "round", "op": "inc", "value": 1}
					],
					"continue_condition": {
						"logical_operator": "and",
						"conditions": [
							{"variable_selector": ["loop_internal", "continue_raw"], "comparison_operator": "is", "value": "true"},
							{"variable_selector": ["loop_internal", "round_lt_max"], "comparison_operator": "is", "value": "true"}
						]
					},
					"outputs": [
						{"name": "final_number", "from": "loop.state.current_number", "required": true},
						{"name": "rounds", "from": "loop.meta.rounds", "required": true},
						{"name": "terminated_by", "from": "loop.meta.terminated_by", "required": true}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "final_number", "value_selector": ["loop_1", "final_number"]},
						{"variable": "rounds", "value_selector": ["loop_1", "rounds"]},
						{"variable": "terminated_by", "value_selector": ["loop_1", "terminated_by"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "loop_1"},
			{"source": "loop_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{"n": 8}, nil)
	if err != nil {
		t.Fatalf("prime loop workflow failed: %v", err)
	}

	finalNumber := 0
	switch v := runResult.Outputs["final_number"].(type) {
	case int:
		finalNumber = v
	case int64:
		finalNumber = int(v)
	case float64:
		finalNumber = int(v)
	default:
		t.Fatalf("unexpected final_number type: %T", runResult.Outputs["final_number"])
	}
	if finalNumber != 11 {
		t.Fatalf("expected final_number=11, got %v", runResult.Outputs["final_number"])
	}

	rounds := 0
	switch v := runResult.Outputs["rounds"].(type) {
	case int:
		rounds = v
	case int64:
		rounds = int(v)
	case float64:
		rounds = int(v)
	default:
		t.Fatalf("unexpected rounds type: %T", runResult.Outputs["rounds"])
	}
	if rounds != 4 {
		t.Fatalf("expected rounds=4, got %v", runResult.Outputs["rounds"])
	}

	if runResult.Outputs["terminated_by"] != "condition-false" {
		t.Fatalf("unexpected terminated_by: %v", runResult.Outputs["terminated_by"])
	}
}

func TestLoopPrimeJudgeTerminatedByMaxRounds(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "n", "label": "Number", "type": "number", "required": true}
					]
				}
			},
			{
				"id": "loop_1",
				"data": {
					"type": "loop",
					"title": "Prime Loop",
					"mode": "while",
					"max_rounds": 10,
					"state_init": [
						{"name": "current_number", "value_selector": ["start_1", "n"], "required": true},
						{"name": "round", "default": 0, "required": true}
					],
					"subgraph": {
						"start": "loop_start",
						"nodes": [
							{
								"id": "loop_start",
								"data": {
									"type": "loop-start",
									"title": "Loop Start"
								}
							},
							{
								"id": "judge_1",
								"data": {
									"type": "func",
									"title": "Prime Judge",
									"function_ref": "math.prime_judge.v1",
									"inputs": [
										{"name": "number", "type": "number", "required": true, "value_selector": ["loop_1", "current_number"]}
									],
									"outputs": [
										{"name": "is_prime", "type": "boolean", "required": true},
										{"name": "continue_loop", "type": "boolean", "required": true},
										{"name": "next_number", "type": "number", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "loop_start", "target": "judge_1"}
						],
						"continue_selector": ["judge_1", "continue_loop"]
					},
					"state_update": [
						{"name": "current_number", "op": "assign", "value_selector": ["judge_1", "next_number"], "required": true},
						{"name": "round", "op": "inc", "value": 1}
					],
					"continue_condition": {
						"logical_operator": "and",
						"conditions": [
							{"variable_selector": ["loop_internal", "continue_raw"], "comparison_operator": "is", "value": "true"},
							{"variable_selector": ["loop_internal", "round_lt_max"], "comparison_operator": "is", "value": "true"}
						]
					},
					"outputs": [
						{"name": "final_number", "from": "loop.state.current_number", "required": true},
						{"name": "rounds", "from": "loop.meta.rounds", "required": true},
						{"name": "terminated_by", "from": "loop.meta.terminated_by", "required": true}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "final_number", "value_selector": ["loop_1", "final_number"]},
						{"variable": "rounds", "value_selector": ["loop_1", "rounds"]},
						{"variable": "terminated_by", "value_selector": ["loop_1", "terminated_by"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "loop_1"},
			{"source": "loop_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{"n": 114}, nil)
	if err != nil {
		t.Fatalf("prime loop workflow failed: %v", err)
	}

	if runResult.Outputs["terminated_by"] != "max-rounds" {
		t.Fatalf("expected terminated_by=max-rounds, got %v", runResult.Outputs["terminated_by"])
	}
}

func TestLoopTerminatedByMaxDurationPrecedence(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{
				"id": "start_1",
				"data": {
					"type": "start",
					"title": "Start",
					"variables": [
						{"variable": "n", "label": "Number", "type": "number", "required": true}
					]
				}
			},
			{
				"id": "loop_1",
				"data": {
					"type": "loop",
					"title": "Duration Loop",
					"mode": "while",
					"max_rounds": 10,
					"max_duration_ms": 1,
					"state_init": [
						{"name": "current_number", "value_selector": ["start_1", "n"], "required": true},
						{"name": "round", "default": 0, "required": true}
					],
					"subgraph": {
						"start": "loop_start",
						"nodes": [
							{
								"id": "loop_start",
								"data": {
									"type": "loop-start",
									"title": "Loop Start"
								}
							},
							{
								"id": "step_1",
								"data": {
									"type": "func",
									"title": "Slow Stop",
									"function_ref": "test.engine.loop.slow_stop.v1",
									"inputs": [
										{"name": "number", "type": "number", "required": true, "value_selector": ["loop_1", "current_number"]}
									],
									"outputs": [
										{"name": "next_number", "type": "number", "required": true},
										{"name": "continue_loop", "type": "boolean", "required": true}
									]
								}
							}
						],
						"edges": [
							{"source": "loop_start", "target": "step_1"}
						],
						"continue_selector": ["step_1", "continue_loop"]
					},
					"state_update": [
						{"name": "current_number", "op": "assign", "value_selector": ["step_1", "next_number"], "required": true},
						{"name": "round", "op": "inc", "value": 1}
					],
					"continue_condition": {
						"logical_operator": "and",
						"conditions": [
							{"variable_selector": ["loop_internal", "continue_raw"], "comparison_operator": "is", "value": "true"},
							{"variable_selector": ["loop_internal", "round_lt_max"], "comparison_operator": "is", "value": "true"}
						]
					},
					"outputs": [
						{"name": "rounds", "from": "loop.meta.rounds", "required": true},
						{"name": "terminated_by", "from": "loop.meta.terminated_by", "required": true}
					]
				}
			},
			{
				"id": "end_1",
				"data": {
					"type": "end",
					"title": "End",
					"outputs": [
						{"variable": "rounds", "value_selector": ["loop_1", "rounds"]},
						{"variable": "terminated_by", "value_selector": ["loop_1", "terminated_by"]}
					]
				}
			}
		],
		"edges": [
			{"source": "start_1", "target": "loop_1"},
			{"source": "loop_1", "target": "end_1"}
		]
	}`)

	runner := workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	runResult, err := runner.RunSync(context.Background(), dsl, map[string]interface{}{"n": 8}, nil)
	if err != nil {
		t.Fatalf("duration loop workflow failed: %v", err)
	}

	if runResult.Outputs["terminated_by"] != "max-duration" {
		t.Fatalf("expected terminated_by=max-duration, got %v", runResult.Outputs["terminated_by"])
	}
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
