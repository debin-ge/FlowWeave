package loop

import "fmt"

const (
	LoopInvalidMode             = "LOOP_INVALID_MODE"
	LoopMaxRoundsInvalid        = "LOOP_MAX_ROUNDS_INVALID"
	LoopStateInitInvalid        = "LOOP_STATE_INIT_INVALID"
	LoopSubgraphInvalid         = "LOOP_SUBGRAPH_INVALID"
	LoopContinueSelectorInvalid = "LOOP_CONTINUE_SELECTOR_INVALID"
	LoopStateUpdateInvalid      = "LOOP_STATE_UPDATE_INVALID"
	LoopOutputsInvalid          = "LOOP_OUTPUTS_INVALID"
	LoopInitStateMissing        = "LOOP_INIT_STATE_MISSING"
	LoopSubgraphExecFailed      = "LOOP_SUBGRAPH_EXEC_FAILED"
	LoopContinueEvalFailed      = "LOOP_CONTINUE_EVAL_FAILED"
	LoopStateUpdateFailed       = "LOOP_STATE_UPDATE_FAILED"
	LoopMaxDurationExceeded     = "LOOP_MAX_DURATION_EXCEEDED"
	LoopPartialFailed           = "LOOP_PARTIAL_FAILED"
)

type LoopNodeError struct {
	Code    string
	Message string
	Cause   error
}

func (e *LoopNodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func newLoopError(code, message string, cause error) error {
	return &LoopNodeError{Code: code, Message: message, Cause: cause}
}
