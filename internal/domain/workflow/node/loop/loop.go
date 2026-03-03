package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/graph"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/runtime"
)

func init() {
	node.Register(types.NodeTypeLoop, NewLoopNode)
	node.Register(types.NodeTypeLoopStart, NewLoopStartNode)
}

type LoopNode struct {
	*node.BaseNode
	data LoopNodeData
}

func NewLoopNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data LoopNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse loop node config: %w", err)
	}
	data.applyDefaults()
	if err := ValidateLoopNodeData(&data); err != nil {
		return nil, err
	}
	return &LoopNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeLoop, data.Title, types.NodeExecutionTypeContainer),
		data:     data,
	}, nil
}

func (n *LoopNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if !ok {
			return nil, newLoopError(LoopSubgraphInvalid, "variable pool not found in context", nil)
		}

		state, err := n.resolveInitialState(vp)
		if err != nil {
			return nil, err
		}

		startAt := time.Now()
		roundStats := make([]map[string]interface{}, 0, n.data.MaxRounds)
		errors := make([]map[string]interface{}, 0)
		executedRounds := 0
		failedRounds := 0
		terminatedBy := "max-rounds"
		status := "succeeded"

		for round := 0; round < n.data.MaxRounds; round++ {
			if n.data.MaxDurationMS > 0 && time.Since(startAt) >= time.Duration(n.data.MaxDurationMS)*time.Millisecond {
				terminatedBy = "max-duration"
				status = "timeout"
				break
			}

			executedRounds++
			roundStart := time.Now()

			continueRaw, nextState, roundErr := n.runRound(ctx, vp, state, round)
			roundDuration := time.Since(roundStart).Milliseconds()
			if roundErr != nil {
				failedRounds++
				errors = append(errors, map[string]interface{}{
					"round": round,
					"error": roundErr.Error(),
				})
				roundStats = append(roundStats, map[string]interface{}{
					"round":       round,
					"status":      "failed",
					"duration_ms": roundDuration,
					"error":       roundErr.Error(),
				})

				if n.data.OnRoundError == loopRoundErrorFailFast {
					return nil, roundErr
				}

				status = "partial-failed"
				if round == n.data.MaxRounds-1 {
					terminatedBy = "max-rounds"
				}
				continue
			}

			state = nextState
			roundLtMax := round+1 < n.data.MaxRounds

			continueDecision, err := n.evalContinue(continueRaw, roundLtMax, state)
			if err != nil {
				return nil, err
			}

			roundStats = append(roundStats, map[string]interface{}{
				"round":           round,
				"status":          "succeeded",
				"duration_ms":     roundDuration,
				"continue_raw":    continueRaw,
				"continue_final":  continueDecision,
				"state_key_count": len(state),
			})

			if n.data.MaxDurationMS > 0 && time.Since(startAt) >= time.Duration(n.data.MaxDurationMS)*time.Millisecond {
				terminatedBy = "max-duration"
				status = "timeout"
				break
			}

			if !continueDecision {
				if continueRaw && !roundLtMax {
					terminatedBy = "max-rounds"
				} else {
					terminatedBy = "condition-false"
				}
				break
			}
		}

		meta := map[string]interface{}{
			"rounds":         executedRounds,
			"failed_rounds":  failedRounds,
			"terminated_by":  terminatedBy,
			"duration_ms":    time.Since(startAt).Milliseconds(),
			"on_round_error": n.data.OnRoundError,
			"status":         status,
			"round_stats":    roundStats,
		}
		if status == "partial-failed" {
			meta["error_code"] = LoopPartialFailed
		}
		if status == "timeout" {
			meta["error_code"] = LoopMaxDurationExceeded
		}

		outputs, err := n.mapOutputs(state, meta, errors)
		if err != nil {
			return nil, err
		}

		return &node.NodeRunResult{
			Status:   types.NodeExecutionStatusSucceeded,
			Outputs:  outputs,
			Metadata: meta,
		}, nil
	})
}

func (n *LoopNode) resolveInitialState(vp node.VariablePoolAccessor) (map[string]interface{}, error) {
	state := make(map[string]interface{}, len(n.data.StateInit))
	for _, item := range n.data.StateInit {
		var (
			val interface{}
			ok  bool
		)
		if len(item.ValueSelector) == 2 {
			val, ok = vp.GetVariable(item.ValueSelector)
		}
		if !ok && item.Default != nil {
			val = item.Default
			ok = true
		}
		if !ok {
			if item.Required {
				return nil, newLoopError(LoopInitStateMissing, "missing required state_init: "+item.Name, nil)
			}
			continue
		}
		state[item.Name] = val
	}
	return state, nil
}

func (n *LoopNode) runRound(ctx context.Context, parentVP node.VariablePoolAccessor, state map[string]interface{}, round int) (bool, map[string]interface{}, error) {
	localVP := cloneVariablePool(parentVP)
	injectLoopContext(localVP, n.ID(), state, round)

	cfg := &types.GraphConfig{Nodes: n.data.Subgraph.Nodes, Edges: n.data.Subgraph.Edges}
	g, err := graph.Init(cfg, node.NewFactory())
	if err != nil {
		return false, nil, newLoopError(LoopSubgraphInvalid, "failed to build subgraph", err)
	}

	stateRuntime := runtime.NewGraphRuntimeState(localVP)
	eng := engine.New(g, stateRuntime, engine.DefaultConfig())
	for evt := range eng.Run(ctx) {
		switch evt.Type {
		case event.EventTypeGraphRunFailed, event.EventTypeGraphRunAborted:
			if evt.Error == "" {
				evt.Error = "subgraph run failed"
			}
			return false, nil, newLoopError(LoopSubgraphExecFailed, evt.Error, nil)
		case event.EventTypeGraphRunSucceeded:
			continueVal, ok := localVP.GetVariable(n.data.Subgraph.ContinueSelector)
			if !ok {
				return false, nil, newLoopError(LoopContinueSelectorInvalid, "continue_selector variable not found", nil)
			}
			continueRaw := toBool(continueVal)
			nextState, err := n.applyStateUpdate(state, localVP)
			if err != nil {
				return false, nil, err
			}
			return continueRaw, nextState, nil
		}
	}

	return false, nil, newLoopError(LoopSubgraphExecFailed, "subgraph finished without terminal event", nil)
}

func (n *LoopNode) applyStateUpdate(current map[string]interface{}, vp *runtime.VariablePool) (map[string]interface{}, error) {
	next := make(map[string]interface{}, len(current))
	for k, v := range current {
		next[k] = v
	}

	for _, up := range n.data.StateUpdate {
		switch up.Op {
		case "assign":
			val, ok := vp.GetVariable(up.ValueSelector)
			if !ok {
				if up.Required {
					return nil, newLoopError(LoopStateUpdateFailed, "missing required state update: "+up.Name, nil)
				}
				continue
			}
			next[up.Name] = val
		case "inc":
			delta := parseInt64(up.Value, 1)
			base := parseInt64(next[up.Name], 0)
			next[up.Name] = base + delta
		default:
			return nil, newLoopError(LoopStateUpdateInvalid, "unsupported state update op: "+up.Op, nil)
		}
	}

	return next, nil
}

func (n *LoopNode) evalContinue(continueRaw, roundLtMax bool, state map[string]interface{}) (bool, error) {
	if len(n.data.ContinueCondition.Comparisons) == 0 {
		return continueRaw && roundLtMax, nil
	}

	vp := runtime.NewVariablePool()
	vp.Set("loop_internal", "continue_raw", continueRaw)
	vp.Set("loop_internal", "round_lt_max", roundLtMax)
	vp.Set(n.ID(), "state", cloneState(state))
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vp.Set(n.ID(), k, state[k])
	}

	return evaluateCondition(n.data.ContinueCondition, vp), nil
}

func evaluateCondition(cond LoopCondition, vp node.VariablePoolAccessor) bool {
	if len(cond.Comparisons) == 0 {
		return false
	}
	isAnd := strings.ToLower(cond.LogicalOp) != "or"
	for _, comp := range cond.Comparisons {
		matched := evaluateComparison(comp, vp)
		if isAnd && !matched {
			return false
		}
		if !isAnd && matched {
			return true
		}
	}
	return isAnd
}

func evaluateComparison(comp LoopComparison, vp node.VariablePoolAccessor) bool {
	if vp == nil {
		return false
	}
	val, ok := vp.GetVariable(comp.VariableSelector)
	if !ok {
		switch comp.Operator {
		case "is-empty", "is-null", "not-exist":
			return true
		default:
			return false
		}
	}

	strVal := fmt.Sprintf("%v", val)
	expected := comp.Value

	switch comp.Operator {
	case "contains":
		return strings.Contains(strVal, expected)
	case "not-contains":
		return !strings.Contains(strVal, expected)
	case "is", "equal":
		return strVal == expected
	case "is-not", "not-equal":
		return strVal != expected
	case "is-empty":
		return strVal == ""
	case "is-not-empty":
		return strVal != ""
	case "is-null", "not-exist":
		return val == nil
	case "is-not-null", "exist":
		return val != nil
	case "gt", ">":
		return toFloat(val) > toFloat(expected)
	case "lt", "<":
		return toFloat(val) < toFloat(expected)
	case "gte", "ge", ">=":
		return toFloat(val) >= toFloat(expected)
	case "lte", "le", "<=":
		return toFloat(val) <= toFloat(expected)
	case "eq", "==":
		return toFloat(val) == toFloat(expected)
	case "ne", "!=":
		return toFloat(val) != toFloat(expected)
	default:
		return false
	}
}

func (n *LoopNode) mapOutputs(state map[string]interface{}, meta map[string]interface{}, errors []map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(n.data.Outputs))
	for _, def := range n.data.Outputs {
		var (
			val interface{}
			ok  bool
		)
		switch {
		case strings.HasPrefix(def.From, loopStatePrefix):
			key := strings.TrimPrefix(def.From, loopStatePrefix)
			val, ok = state[key]
		case strings.HasPrefix(def.From, loopMetaPrefix):
			key := strings.TrimPrefix(def.From, loopMetaPrefix)
			val, ok = meta[key]
		case def.From == loopErrorsFrom:
			val = errors
			ok = true
		}

		if !ok {
			if def.Required {
				return nil, newLoopError(LoopOutputsInvalid, "required output mapping missing: "+def.From, nil)
			}
			continue
		}
		out[def.Name] = val
	}

	return out, nil
}

func cloneVariablePool(vp node.VariablePoolAccessor) *runtime.VariablePool {
	cloned := runtime.NewVariablePool()
	rvp, ok := vp.(*runtime.VariablePool)
	if !ok {
		return cloned
	}

	dump := rvp.Dump()
	for nodeID, raw := range dump {
		if nodeID == "__system__" {
			sysVars, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			for k, v := range sysVars {
				cloned.SetSystem(k, v)
			}
			continue
		}
		nodeVars, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		for k, v := range nodeVars {
			cloned.Set(nodeID, k, v)
		}
	}
	return cloned
}

func injectLoopContext(vp *runtime.VariablePool, nodeID string, state map[string]interface{}, round int) {
	vp.Set(nodeID, "state", cloneState(state))
	for k, v := range state {
		vp.Set(nodeID, k, v)
	}
	vp.Set(nodeID, "round", round)
}

func cloneState(state map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(state))
	for k, v := range state {
		cp[k] = v
	}
	return cp
}

func parseInt64(v interface{}, def int64) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n)
	case uint8:
		return int64(n)
	case uint16:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return def
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case uint:
		return float64(val)
	case uint64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

type LoopStartNode struct {
	*node.BaseNode
}

func NewLoopStartNode(id string, raw json.RawMessage) (node.Node, error) {
	var nd types.NodeData
	if err := json.Unmarshal(raw, &nd); err != nil {
		return nil, err
	}
	return &LoopStartNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeLoopStart, nd.Title, types.NodeExecutionTypeRoot),
	}, nil
}

func (n *LoopStartNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{},
		}, nil
	})
}
