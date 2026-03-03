package loop

import types "flowweave/internal/domain/workflow/model"

const (
	defaultLoopMode       = "while"
	defaultOnRoundError   = "fail-fast"
	defaultStateUpdateOp  = "assign"
	defaultConditionLogic = "and"
)

const (
	loopModeWhile = "while"
)

const (
	loopRoundErrorFailFast = "fail-fast"
	loopRoundErrorContinue = "continue"
)

const (
	loopStatePrefix = "loop.state."
	loopMetaPrefix  = "loop.meta."
	loopErrorsFrom  = "loop.errors"
)

type LoopNodeData struct {
	Type              string            `json:"type"`
	Title             string            `json:"title"`
	Mode              string            `json:"mode"`
	MaxRounds         int               `json:"max_rounds"`
	MaxDurationMS     int               `json:"max_duration_ms,omitempty"`
	OnRoundError      string            `json:"on_round_error,omitempty"`
	StateInit         []LoopStateInit   `json:"state_init"`
	Subgraph          LoopSubgraph      `json:"subgraph"`
	StateUpdate       []LoopStateUpdate `json:"state_update"`
	ContinueCondition LoopCondition     `json:"continue_condition"`
	Outputs           []LoopOutput      `json:"outputs"`
}

type LoopStateInit struct {
	Name          string                 `json:"name"`
	ValueSelector types.VariableSelector `json:"value_selector,omitempty"`
	Default       interface{}            `json:"default,omitempty"`
	Required      bool                   `json:"required,omitempty"`
}

type LoopSubgraph struct {
	Start            string                 `json:"start"`
	Nodes            []types.NodeConfig     `json:"nodes"`
	Edges            []types.EdgeConfig     `json:"edges"`
	ContinueSelector types.VariableSelector `json:"continue_selector"`
}

type LoopStateUpdate struct {
	Name          string                 `json:"name"`
	ValueSelector types.VariableSelector `json:"value_selector,omitempty"`
	Op            string                 `json:"op,omitempty"` // assign|inc
	Value         interface{}            `json:"value,omitempty"`
	Required      bool                   `json:"required,omitempty"`
}

type LoopCondition struct {
	LogicalOp   string           `json:"logical_operator"`
	Comparisons []LoopComparison `json:"conditions"`
}

type LoopComparison struct {
	VariableSelector types.VariableSelector `json:"variable_selector"`
	Operator         string                 `json:"comparison_operator"`
	Value            string                 `json:"value"`
}

type LoopOutput struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	Required bool   `json:"required,omitempty"`
}

func (d *LoopNodeData) applyDefaults() {
	if d.Mode == "" {
		d.Mode = defaultLoopMode
	}
	if d.OnRoundError == "" {
		d.OnRoundError = defaultOnRoundError
	}
	for i := range d.StateUpdate {
		if d.StateUpdate[i].Op == "" {
			d.StateUpdate[i].Op = defaultStateUpdateOp
		}
	}
	if d.ContinueCondition.LogicalOp == "" {
		d.ContinueCondition.LogicalOp = defaultConditionLogic
	}
}
