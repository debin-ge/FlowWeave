package iteration

import (
	"fmt"
	"regexp"
)

var variableNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func ValidateIterationNodeData(d *IterationNodeData) error {
	if d == nil {
		return newIterationError(IterationSubgraphInvalid, "iteration config is nil", nil)
	}
	if d.Mode != "map" {
		return newIterationError(IterationInvalidMode, "iteration mode must be 'map'", nil)
	}
	if len(d.Input.ValueSelector) != 2 {
		return newIterationError(IterationInputSelectorInvalid, "input.value_selector must be [node_id, var_name]", nil)
	}
	if len(d.Subgraph.Nodes) == 0 {
		return newIterationError(IterationSubgraphInvalid, "subgraph.nodes must be non-empty", nil)
	}
	if d.Subgraph.Start == "" {
		return newIterationError(IterationSubgraphMissingStart, "subgraph.start is required", nil)
	}
	if len(d.Subgraph.ResultSelector) != 2 {
		return newIterationError(IterationResultSelectorInvalid, "subgraph.result_selector must be [node_id, var_name]", nil)
	}

	nodeIDs := make(map[string]struct{}, len(d.Subgraph.Nodes))
	for _, n := range d.Subgraph.Nodes {
		if n.ID == "" {
			return newIterationError(IterationSubgraphInvalid, "subgraph node id must be non-empty", nil)
		}
		if _, ok := nodeIDs[n.ID]; ok {
			return newIterationError(IterationSubgraphInvalid, fmt.Sprintf("duplicate subgraph node id: %s", n.ID), nil)
		}
		nodeIDs[n.ID] = struct{}{}
	}
	if _, ok := nodeIDs[d.Subgraph.Start]; !ok {
		return newIterationError(IterationSubgraphMissingStart, "subgraph.start not found in subgraph.nodes", nil)
	}

	if d.Concurrency == nil || d.Concurrency.MaxConcurrency < 1 {
		return newIterationError(IterationConcurrencyInvalid, "max_concurrency must be >= 1", nil)
	}
	if d.Concurrency.Order != orderInput && d.Concurrency.Order != orderCompletion {
		return newIterationError(IterationConcurrencyInvalid, "concurrency.order must be input-order or completion-order", nil)
	}

	switch d.Aggregate.Strategy {
	case aggregateStrategyCollect:
	case aggregateStrategyReduce:
		if d.Aggregate.Reduce == nil {
			return newIterationError(IterationAggregateInvalid, "aggregate.reduce is required when strategy=reduce", nil)
		}
	default:
		return newIterationError(IterationAggregateInvalid, "aggregate.strategy must be collect/reduce", nil)
	}

	if d.OnItemError != itemErrorFailFast && d.OnItemError != itemErrorContinue {
		return newIterationError(IterationAggregateInvalid, "on_item_error must be fail-fast or continue", nil)
	}
	if d.MaxItems < 0 {
		return newIterationError(IterationAggregateInvalid, "max_items must be >= 0", nil)
	}
	if len(d.Outputs) == 0 {
		return newIterationError(IterationOutputsInvalid, "outputs must be non-empty", nil)
	}
	outputNames := make(map[string]struct{}, len(d.Outputs))
	for _, out := range d.Outputs {
		if out.Name == "" || !variableNamePattern.MatchString(out.Name) {
			return newIterationError(IterationOutputsInvalid, "outputs.name must be valid variable name", nil)
		}
		if _, ok := outputNames[out.Name]; ok {
			return newIterationError(IterationOutputsInvalid, fmt.Sprintf("duplicate output name: %s", out.Name), nil)
		}
		outputNames[out.Name] = struct{}{}
		switch out.From {
		case "aggregate.result", "aggregate.meta", "aggregate.errors":
		default:
			return newIterationError(IterationOutputsInvalid, "outputs.from must be aggregate.result/meta/errors", nil)
		}
	}

	if d.Context == nil {
		return nil
	}
	if err := validateContextKey(d.Context.ItemKey); err != nil {
		return err
	}
	if err := validateContextKey(d.Context.IndexKey); err != nil {
		return err
	}
	if err := validateContextKey(d.Context.FirstKey); err != nil {
		return err
	}
	if err := validateContextKey(d.Context.LastKey); err != nil {
		return err
	}

	return nil
}

func validateContextKey(key string) error {
	if key == "" {
		return nil
	}
	if !variableNamePattern.MatchString(key) {
		return newIterationError(IterationAggregateInvalid, fmt.Sprintf("invalid context key: %s", key), nil)
	}
	return nil
}
