package loop

import (
	"fmt"
	"regexp"
	"strings"
)

var variableNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func ValidateLoopNodeData(d *LoopNodeData) error {
	if d == nil {
		return newLoopError(LoopSubgraphInvalid, "loop config is nil", nil)
	}
	if d.Mode != loopModeWhile {
		return newLoopError(LoopInvalidMode, "loop mode must be 'while'", nil)
	}
	if d.MaxRounds < 1 {
		return newLoopError(LoopMaxRoundsInvalid, "max_rounds must be >= 1", nil)
	}
	if d.MaxDurationMS < 0 {
		return newLoopError(LoopMaxRoundsInvalid, "max_duration_ms must be >= 0", nil)
	}

	if len(d.StateInit) == 0 {
		return newLoopError(LoopStateInitInvalid, "state_init must be non-empty", nil)
	}
	stateNames := make(map[string]struct{}, len(d.StateInit))
	for _, st := range d.StateInit {
		if st.Name == "" || !variableNamePattern.MatchString(st.Name) {
			return newLoopError(LoopStateInitInvalid, "state_init.name must be valid variable name", nil)
		}
		if _, ok := stateNames[st.Name]; ok {
			return newLoopError(LoopStateInitInvalid, fmt.Sprintf("duplicate state_init name: %s", st.Name), nil)
		}
		stateNames[st.Name] = struct{}{}
		if len(st.ValueSelector) > 0 && len(st.ValueSelector) != 2 {
			return newLoopError(LoopStateInitInvalid, "state_init.value_selector must be [node_id, var_name]", nil)
		}
	}

	if d.Subgraph.Start == "" {
		return newLoopError(LoopSubgraphInvalid, "subgraph.start is required", nil)
	}
	if len(d.Subgraph.Nodes) == 0 {
		return newLoopError(LoopSubgraphInvalid, "subgraph.nodes must be non-empty", nil)
	}
	nodeIDs := make(map[string]struct{}, len(d.Subgraph.Nodes))
	for _, n := range d.Subgraph.Nodes {
		if n.ID == "" {
			return newLoopError(LoopSubgraphInvalid, "subgraph node id must be non-empty", nil)
		}
		if _, ok := nodeIDs[n.ID]; ok {
			return newLoopError(LoopSubgraphInvalid, fmt.Sprintf("duplicate subgraph node id: %s", n.ID), nil)
		}
		nodeIDs[n.ID] = struct{}{}
	}
	if _, ok := nodeIDs[d.Subgraph.Start]; !ok {
		return newLoopError(LoopSubgraphInvalid, "subgraph.start not found in subgraph.nodes", nil)
	}
	if len(d.Subgraph.ContinueSelector) != 2 {
		return newLoopError(LoopContinueSelectorInvalid, "subgraph.continue_selector must be [node_id, var_name]", nil)
	}

	if len(d.StateUpdate) == 0 {
		return newLoopError(LoopStateUpdateInvalid, "state_update must be non-empty", nil)
	}
	updateNames := make(map[string]struct{}, len(d.StateUpdate))
	for _, up := range d.StateUpdate {
		if up.Name == "" || !variableNamePattern.MatchString(up.Name) {
			return newLoopError(LoopStateUpdateInvalid, "state_update.name must be valid variable name", nil)
		}
		if _, ok := updateNames[up.Name]; ok {
			return newLoopError(LoopStateUpdateInvalid, fmt.Sprintf("duplicate state_update name: %s", up.Name), nil)
		}
		updateNames[up.Name] = struct{}{}
		switch up.Op {
		case "assign", "inc":
		default:
			return newLoopError(LoopStateUpdateInvalid, "state_update.op must be assign or inc", nil)
		}
		if up.Op == "assign" && len(up.ValueSelector) != 2 {
			return newLoopError(LoopStateUpdateInvalid, "assign update requires value_selector [node_id, var_name]", nil)
		}
	}

	logical := strings.ToLower(d.ContinueCondition.LogicalOp)
	if logical != "and" && logical != "or" {
		return newLoopError(LoopContinueEvalFailed, "continue_condition.logical_operator must be and/or", nil)
	}
	for _, c := range d.ContinueCondition.Comparisons {
		if len(c.VariableSelector) != 2 {
			return newLoopError(LoopContinueEvalFailed, "continue_condition.variable_selector must be [node_id, var_name]", nil)
		}
	}

	if len(d.Outputs) == 0 {
		return newLoopError(LoopOutputsInvalid, "outputs must be non-empty", nil)
	}
	outputNames := make(map[string]struct{}, len(d.Outputs))
	for _, out := range d.Outputs {
		if out.Name == "" || !variableNamePattern.MatchString(out.Name) {
			return newLoopError(LoopOutputsInvalid, "outputs.name must be valid variable name", nil)
		}
		if _, ok := outputNames[out.Name]; ok {
			return newLoopError(LoopOutputsInvalid, fmt.Sprintf("duplicate output name: %s", out.Name), nil)
		}
		outputNames[out.Name] = struct{}{}

		valid := strings.HasPrefix(out.From, loopStatePrefix) || strings.HasPrefix(out.From, loopMetaPrefix) || out.From == loopErrorsFrom
		if !valid {
			return newLoopError(LoopOutputsInvalid, "outputs.from must be loop.state.*, loop.meta.*, or loop.errors", nil)
		}
	}

	return nil
}
