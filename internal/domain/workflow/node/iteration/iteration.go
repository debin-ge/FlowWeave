package iteration

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/graph"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/domain/workflow/runtime"
)

func init() {
	node.Register(types.NodeTypeIteration, NewIterationNode)
	node.Register(types.NodeTypeIterationStart, NewIterationStartNode)
}

// IterationNode executes a configurable subgraph for each item in the input list.
type IterationNode struct {
	*node.BaseNode
	data IterationNodeData
}

func NewIterationNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data IterationNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse iteration node config: %w", err)
	}
	data.applyDefaults()
	if err := ValidateIterationNodeData(&data); err != nil {
		return nil, err
	}

	return &IterationNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeIteration, data.Title, types.NodeExecutionTypeContainer),
		data:     data,
	}, nil
}

func (n *IterationNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if !ok {
			return nil, newIterationError(IterationSubgraphInvalid, "variable pool not found in context", nil)
		}

		listValue, found := vp.GetVariable(n.data.Input.ValueSelector)
		if !found {
			if n.data.inputRequired() {
				return nil, newIterationError(IterationInputNotFound, "iteration input not found", nil)
			}
			return n.emptyResult(), nil
		}

		items, err := toSlice(listValue)
		if err != nil {
			return nil, newIterationError(IterationInputNotArray, "iteration input must be array/list", err)
		}
		if n.data.MaxItems > 0 && len(items) > n.data.MaxItems {
			items = items[:n.data.MaxItems]
		}
		if len(items) == 0 {
			return n.emptyResult(), nil
		}

		startAt := time.Now()
		runs := n.executeItems(ctx, vp, items)
		if n.data.Concurrency.Order == orderInput {
			sort.Slice(runs, func(i, j int) bool { return runs[i].Index < runs[j].Index })
		}

		aggResult, aggErr := n.aggregate(runs)
		if aggErr != nil {
			return nil, aggErr
		}

		meta := buildMeta(runs, n.data.Concurrency.MaxConcurrency, n.data.Concurrency.Order, time.Since(startAt))
		if n.data.OnItemError == itemErrorContinue && meta["failed"].(int) > 0 {
			meta["status"] = IterationPartialFailed
		}

		mappedOutputs, err := n.mapOutputs(aggResult, meta)
		if err != nil {
			return nil, err
		}

		return &node.NodeRunResult{
			Status:   types.NodeExecutionStatusSucceeded,
			Outputs:  mappedOutputs,
			Metadata: meta,
		}, nil
	})
}

func (n *IterationNode) emptyResult() *node.NodeRunResult {
	meta := map[string]interface{}{
		"total":           0,
		"success":         0,
		"failed":          0,
		"skipped":         0,
		"duration_ms":     int64(0),
		"max_concurrency": n.data.Concurrency.MaxConcurrency,
		"order":           n.data.Concurrency.Order,
		"errors":          []map[string]interface{}{},
	}
	aggregate := map[string]interface{}{
		"aggregate.result": []interface{}{},
		"aggregate.errors": []map[string]interface{}{},
	}
	mappedOutputs, _ := n.mapOutputs(aggregate, meta)
	return &node.NodeRunResult{
		Status:   types.NodeExecutionStatusSucceeded,
		Outputs:  mappedOutputs,
		Metadata: meta,
	}
}

type itemRunResult struct {
	Index int
	Value interface{}
	Err   error
}

func (n *IterationNode) executeItems(ctx context.Context, parentVP node.VariablePoolAccessor, items []interface{}) []itemRunResult {
	results := make([]itemRunResult, 0, len(items))
	resultCh := make(chan itemRunResult, len(items))
	sem := make(chan struct{}, n.data.Concurrency.MaxConcurrency)
	itemCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, it interface{}) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-itemCtx.Done():
				return
			}
			defer func() { <-sem }()

			val, err := n.runSubgraphForItem(itemCtx, parentVP, it, idx, len(items))
			if err != nil {
				if n.data.OnItemError == itemErrorFailFast {
					cancel()
				}
				resultCh <- itemRunResult{Index: idx, Err: err}
				return
			}
			resultCh <- itemRunResult{Index: idx, Value: val}
		}(i, item)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		results = append(results, r)
	}

	return results
}

func (n *IterationNode) runSubgraphForItem(ctx context.Context, parentVP node.VariablePoolAccessor, item interface{}, index, total int) (interface{}, error) {
	localVP := cloneVariablePool(parentVP)
	injectItemContext(localVP, n.ID(), n.data.Context, item, index, total)

	cfg := &types.GraphConfig{Nodes: n.data.Subgraph.Nodes, Edges: n.data.Subgraph.Edges}
	g, err := graph.Init(cfg, node.NewFactory())
	if err != nil {
		return nil, newIterationError(IterationSubgraphInvalid, "failed to build subgraph", err)
	}

	state := runtime.NewGraphRuntimeState(localVP)
	eng := engine.New(g, state, engine.DefaultConfig())

	for evt := range eng.Run(ctx) {
		switch evt.Type {
		case event.EventTypeGraphRunFailed, event.EventTypeGraphRunAborted:
			if evt.Error == "" {
				evt.Error = "subgraph run failed"
			}
			return nil, newIterationError(IterationSubgraphExecFailed, evt.Error, nil)
		case event.EventTypeGraphRunSucceeded:
			val, ok := localVP.GetVariable(n.data.Subgraph.ResultSelector)
			if !ok {
				return nil, newIterationError(IterationSubgraphExecFailed, "subgraph result_selector not found", nil)
			}
			return val, nil
		}
	}

	return nil, newIterationError(IterationSubgraphExecFailed, "subgraph finished without terminal event", nil)
}

func (n *IterationNode) aggregate(runs []itemRunResult) (map[string]interface{}, error) {
	successValues := make([]interface{}, 0, len(runs))
	errors := make([]map[string]interface{}, 0)
	for _, r := range runs {
		if r.Err != nil {
			errors = append(errors, map[string]interface{}{"index": r.Index, "error": r.Err.Error()})
			if n.data.OnItemError == itemErrorFailFast {
				return nil, newIterationError(IterationItemExecFailed, fmt.Sprintf("item execution failed at index=%d", r.Index), r.Err)
			}
			continue
		}
		successValues = append(successValues, r.Value)
	}

	artifacts := map[string]interface{}{
		"aggregate.result": nil,
		"aggregate.errors": errors,
	}

	switch n.data.Aggregate.Strategy {
	case aggregateStrategyCollect:
		artifacts["aggregate.result"] = n.buildCollectResults(successValues)
	case aggregateStrategyReduce:
		acc := n.data.Aggregate.Reduce.AccInit
		for _, v := range successValues {
			next, err := reduceInline(acc, v, n.data.Aggregate.Reduce)
			if err != nil {
				return nil, newIterationError(IterationReduceExecFailed, "reduce failed", err)
			}
			acc = next
		}
		artifacts["aggregate.result"] = acc
	}

	return artifacts, nil
}

func (n *IterationNode) buildCollectResults(values []interface{}) []interface{} {
	field := n.data.Subgraph.ResultSelector.VarName()
	if field == "" {
		field = "result"
	}
	out := make([]interface{}, 0, len(values))
	for _, v := range values {
		out = append(out, map[string]interface{}{field: v})
	}
	return out
}

func reduceInline(acc interface{}, item interface{}, cfg *IterationReduceConfig) (interface{}, error) {
	if cfg == nil {
		return nil, fmt.Errorf("reduce config is nil")
	}

	// number reduce: sum
	if isNumber(acc) {
		accN, err := toFloat64(acc)
		if err != nil {
			return nil, err
		}
		itemN, err := toFloat64(item)
		if err != nil {
			return nil, err
		}
		return accN + itemN, nil
	}

	// string reduce: join
	if accStr, ok := acc.(string); ok {
		itemStr, err := extractTextValue(item)
		if err != nil {
			return nil, err
		}
		if accStr == "" {
			return itemStr, nil
		}
		return accStr + cfg.JoinWith + itemStr, nil
	}

	// object reduce: join item text into target field (default: text)
	if accMap, ok := acc.(map[string]interface{}); ok {
		field := cfg.TargetField
		if field == "" {
			field = defaultReduceField
		}
		join := cfg.JoinWith
		if join == "" {
			join = defaultReduceJoiner
		}

		itemStr, err := extractTextValue(item)
		if err != nil {
			return nil, err
		}
		prev, _ := accMap[field].(string)
		if prev == "" {
			accMap[field] = itemStr
		} else {
			accMap[field] = prev + join + itemStr
		}
		return accMap, nil
	}

	return nil, fmt.Errorf("unsupported acc_init type: %T", acc)
}

func extractTextValue(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case map[string]interface{}:
		if s, ok := val["text"].(string); ok {
			return s, nil
		}
		if s, ok := val["result"].(string); ok {
			return s, nil
		}
		if nested, ok := val["result"].(map[string]interface{}); ok {
			if s, ok := nested["text"].(string); ok {
				return s, nil
			}
			if s, ok := nested["item"].(string); ok {
				return s, nil
			}
			if itemMap, ok := nested["item"].(map[string]interface{}); ok {
				if s, ok := itemMap["text"].(string); ok {
					return s, nil
				}
			}
		}
		if s, ok := val["item"].(string); ok {
			return s, nil
		}
		if itemMap, ok := val["item"].(map[string]interface{}); ok {
			if s, ok := itemMap["text"].(string); ok {
				return s, nil
			}
		}
		return "", fmt.Errorf("cannot extract text from map item")
	default:
		// last fallback for scalar data
		return fmt.Sprintf("%v", v), nil
	}
}

func isNumber(v interface{}) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func (n *IterationNode) mapOutputs(aggregate map[string]interface{}, meta map[string]interface{}) (map[string]interface{}, error) {
	outputs := make(map[string]interface{}, len(n.data.Outputs))
	aggregate["aggregate.meta"] = meta

	for _, out := range n.data.Outputs {
		val, ok := aggregate[out.From]
		if !ok {
			if out.Required {
				return nil, newIterationError(IterationOutputsInvalid, "required output mapping missing: "+out.From, nil)
			}
			continue
		}
		outputs[out.Name] = val
	}

	return outputs, nil
}

func buildMeta(runs []itemRunResult, maxConcurrency int, order string, duration time.Duration) map[string]interface{} {
	meta := map[string]interface{}{
		"total":           len(runs),
		"success":         0,
		"failed":          0,
		"skipped":         0,
		"duration_ms":     duration.Milliseconds(),
		"max_concurrency": maxConcurrency,
		"order":           order,
		"errors":          []map[string]interface{}{},
	}

	errors := make([]map[string]interface{}, 0)
	success := 0
	failed := 0
	for _, r := range runs {
		if r.Err != nil {
			failed++
			errors = append(errors, map[string]interface{}{"index": r.Index, "error": r.Err.Error()})
			continue
		}
		success++
	}
	meta["success"] = success
	meta["failed"] = failed
	meta["errors"] = errors
	return meta
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

func injectItemContext(vp *runtime.VariablePool, nodeID string, ctxCfg *IterationContext, item interface{}, index, total int) {
	vp.Set(nodeID, ctxCfg.ItemKey, item)
	vp.Set(nodeID, ctxCfg.IndexKey, index)
	vp.Set(nodeID, ctxCfg.FirstKey, index == 0)
	vp.Set(nodeID, ctxCfg.LastKey, index == total-1)
}

// IterationStartNode is an entry node for iteration subgraph.
type IterationStartNode struct {
	*node.BaseNode
}

func NewIterationStartNode(id string, data json.RawMessage) (node.Node, error) {
	var nd types.NodeData
	if err := json.Unmarshal(data, &nd); err != nil {
		return nil, err
	}
	return &IterationStartNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeIterationStart, nd.Title, types.NodeExecutionTypeRoot),
	}, nil
}

func (n *IterationStartNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		return &node.NodeRunResult{Status: types.NodeExecutionStatusSucceeded, Outputs: map[string]interface{}{}}, nil
	})
}

func toSlice(v interface{}) ([]interface{}, error) {
	switch val := v.(type) {
	case []interface{}:
		return val, nil
	case []string:
		result := make([]interface{}, len(val))
		for i, s := range val {
			result[i] = s
		}
		return result, nil
	case []int:
		result := make([]interface{}, len(val))
		for i, n := range val {
			result[i] = n
		}
		return result, nil
	case []float64:
		result := make([]interface{}, len(val))
		for i, n := range val {
			result[i] = n
		}
		return result, nil
	case []map[string]interface{}:
		result := make([]interface{}, len(val))
		for i, m := range val {
			result[i] = m
		}
		return result, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to slice", v)
	}
}
