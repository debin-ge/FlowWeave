package code

import (
	"time"

	types "flowweave/internal/domain/workflow/model"
)

const (
	defaultCodeTimeoutMS = 3000
)

// CodeNodeData Code node DSL config.
type CodeNodeData struct {
	Type         string              `json:"type"`
	Title        string              `json:"title"`
	FunctionRef  string              `json:"function_ref"`
	TimeoutMS    int                 `json:"timeout_ms,omitempty"`
	StrictSchema *bool               `json:"strict_schema,omitempty"`
	Inputs       []CodeInputBinding  `json:"inputs"`
	Outputs      []CodeOutputBinding `json:"outputs"`
}

// CodeInputBinding defines one input binding for the function.
type CodeInputBinding struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Required      bool                   `json:"required"`
	ValueSelector types.VariableSelector `json:"value_selector,omitempty"`
	Default       interface{}            `json:"default,omitempty"`
}

// CodeOutputBinding defines one output field contract.
type CodeOutputBinding struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func (d *CodeNodeData) GetStrictSchema() bool {
	if d != nil && d.StrictSchema != nil {
		return *d.StrictSchema
	}
	return true
}

func (d *CodeNodeData) GetTimeout() time.Duration {
	ms := defaultCodeTimeoutMS
	if d != nil && d.TimeoutMS > 0 {
		ms = d.TimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}
