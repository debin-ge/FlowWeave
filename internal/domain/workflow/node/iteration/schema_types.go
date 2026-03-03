package iteration

import types "flowweave/internal/domain/workflow/model"

const (
	defaultItemKey        = "item"
	defaultIndexKey       = "index"
	defaultFirstKey       = "is_first"
	defaultLastKey        = "is_last"
	defaultOnItemError    = "fail-fast"
	defaultOrder          = "input-order"
	defaultMaxConcurrency = 1
	defaultReduceJoiner   = ","
	defaultReduceField    = "text"
)

const (
	aggregateStrategyCollect = "collect"
	aggregateStrategyReduce  = "reduce"
)

const (
	itemErrorFailFast = "fail-fast"
	itemErrorContinue = "continue"
)

const (
	orderInput      = "input-order"
	orderCompletion = "completion-order"
)

// IterationNodeData defines iteration node DSL config.
type IterationNodeData struct {
	Type        string                `json:"type"`
	Title       string                `json:"title"`
	Mode        string                `json:"mode"`
	Input       IterationInput        `json:"input"`
	Context     *IterationContext     `json:"context,omitempty"`
	Subgraph    IterationSubgraph     `json:"subgraph"`
	Concurrency *IterationConcurrency `json:"concurrency,omitempty"`
	Aggregate   IterationAggregate    `json:"aggregate"`
	Outputs     []IterationOutput     `json:"outputs"`
	OnItemError string                `json:"on_item_error,omitempty"`
	MaxItems    int                   `json:"max_items,omitempty"`
}

type IterationInput struct {
	ValueSelector types.VariableSelector `json:"value_selector"`
	Required      *bool                  `json:"required,omitempty"`
}

type IterationContext struct {
	ItemKey  string `json:"item_key,omitempty"`
	IndexKey string `json:"index_key,omitempty"`
	FirstKey string `json:"first_key,omitempty"`
	LastKey  string `json:"last_key,omitempty"`
}

type IterationSubgraph struct {
	Start          string                 `json:"start"`
	Nodes          []types.NodeConfig     `json:"nodes"`
	Edges          []types.EdgeConfig     `json:"edges"`
	ResultSelector types.VariableSelector `json:"result_selector"`
}

type IterationConcurrency struct {
	MaxConcurrency int    `json:"max_concurrency,omitempty"`
	Order          string `json:"order,omitempty"`
}

type IterationAggregate struct {
	Strategy string                 `json:"strategy"`
	Reduce   *IterationReduceConfig `json:"reduce,omitempty"`
}

type IterationReduceConfig struct {
	AccInit     interface{} `json:"acc_init"`
	JoinWith    string      `json:"join_with,omitempty"`
	TargetField string      `json:"target_field,omitempty"`
}

type IterationOutput struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	Required bool   `json:"required,omitempty"`
}

func (d *IterationNodeData) applyDefaults() {
	if d.Context == nil {
		d.Context = &IterationContext{}
	}
	if d.Context.ItemKey == "" {
		d.Context.ItemKey = defaultItemKey
	}
	if d.Context.IndexKey == "" {
		d.Context.IndexKey = defaultIndexKey
	}
	if d.Context.FirstKey == "" {
		d.Context.FirstKey = defaultFirstKey
	}
	if d.Context.LastKey == "" {
		d.Context.LastKey = defaultLastKey
	}
	if d.Concurrency == nil {
		d.Concurrency = &IterationConcurrency{}
	}
	if d.Concurrency.MaxConcurrency <= 0 {
		d.Concurrency.MaxConcurrency = defaultMaxConcurrency
	}
	if d.Concurrency.Order == "" {
		d.Concurrency.Order = defaultOrder
	}
	if d.OnItemError == "" {
		d.OnItemError = defaultOnItemError
	}
	if d.Aggregate.Reduce != nil {
		if d.Aggregate.Reduce.JoinWith == "" {
			d.Aggregate.Reduce.JoinWith = defaultReduceJoiner
		}
		if d.Aggregate.Reduce.TargetField == "" {
			d.Aggregate.Reduce.TargetField = defaultReduceField
		}
	}
}

func (d *IterationNodeData) inputRequired() bool {
	if d.Input.Required == nil {
		return true
	}
	return *d.Input.Required
}
