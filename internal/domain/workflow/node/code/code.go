package code

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// CodeNode executes registered local functions with strict I/O schema.
type CodeNode struct {
	*node.BaseNode
	data CodeNodeData
}

func init() {
	node.Register(types.NodeTypeFunc, NewCodeNode)
}

// NewCodeNode creates a Code node from DSL data.
func NewCodeNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data CodeNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if err := ValidateCodeNodeData(&data); err != nil {
		return nil, err
	}

	return &CodeNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeFunc, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
	}, nil
}

// Run executes the configured local function.
func (n *CodeNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if !ok {
			return failResult(NewError(CodeNodeInvalidConfig, "variable pool not found in context", nil)), nil
		}

		inputs, err := BuildAndValidateInputs(vp, n.data.Inputs)
		if err != nil {
			return failResult(err), nil
		}

		fn, ok := GetFunction(n.data.FunctionRef)
		if !ok {
			return failResult(NewError(CodeNodeFunctionNotFound, "function not found: "+n.data.FunctionRef, nil)), nil
		}

		timeout := n.data.GetTimeout()
		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		start := time.Now()
		rawOutputs, err := fn.Execute(execCtx, inputs)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
				return failResult(NewError(CodeNodeExecTimeout, "function execution timeout", err)), nil
			}
			return failResult(NewError(CodeNodeExecFailed, "function execution failed", err)), nil
		}

		outputs, err := ValidateAndFilterOutputs(rawOutputs, n.data.Outputs, n.data.GetStrictSchema())
		if err != nil {
			return failResult(err), nil
		}

		return &node.NodeRunResult{
			Status:  types.NodeExecutionStatusSucceeded,
			Outputs: outputs,
			Metadata: map[string]interface{}{
				"function_ref":      n.data.FunctionRef,
				"timeout_ms":        timeout.Milliseconds(),
				"strict_schema":     n.data.GetStrictSchema(),
				"elapsed_ms":        elapsed,
				"input_count":       len(inputs),
				"output_count":      len(outputs),
				"input_validation":  "passed",
				"output_validation": "passed",
			},
		}, nil
	})
}

func failResult(err error) *node.NodeRunResult {
	return &node.NodeRunResult{
		Status: types.NodeExecutionStatusFailed,
		Error:  err.Error(),
	}
}
