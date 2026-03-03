package iteration

import "fmt"

const (
	IterationInvalidMode           = "ITERATION_INVALID_MODE"
	IterationInputSelectorInvalid  = "ITERATION_INPUT_SELECTOR_INVALID"
	IterationSubgraphMissingStart  = "ITERATION_SUBGRAPH_MISSING_START"
	IterationSubgraphInvalid       = "ITERATION_SUBGRAPH_INVALID"
	IterationResultSelectorInvalid = "ITERATION_RESULT_SELECTOR_INVALID"
	IterationConcurrencyInvalid    = "ITERATION_CONCURRENCY_INVALID"
	IterationAggregateInvalid      = "ITERATION_AGGREGATE_INVALID"
	IterationOutputsInvalid        = "ITERATION_OUTPUTS_INVALID"
	IterationReducerNotFound       = "ITERATION_REDUCER_NOT_FOUND"
	IterationInputNotFound         = "ITERATION_INPUT_NOT_FOUND"
	IterationInputNotArray         = "ITERATION_INPUT_NOT_ARRAY"
	IterationItemExecFailed        = "ITERATION_ITEM_EXEC_FAILED"
	IterationSubgraphExecFailed    = "ITERATION_SUBGRAPH_EXEC_FAILED"
	IterationMergeConflict         = "ITERATION_MERGE_CONFLICT"
	IterationReduceExecFailed      = "ITERATION_REDUCE_EXEC_FAILED"
	IterationContextCanceled       = "ITERATION_CONTEXT_CANCELED"
	IterationPartialFailed         = "ITERATION_PARTIAL_FAILED"
)

// IterationNodeError wraps iteration node specific errors.
type IterationNodeError struct {
	Code    string
	Message string
	Cause   error
}

func (e *IterationNodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func newIterationError(code, message string, cause error) error {
	return &IterationNodeError{Code: code, Message: message, Cause: cause}
}
