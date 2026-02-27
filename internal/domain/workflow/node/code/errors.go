package code

import "fmt"

type ErrorCode string

const (
	CodeNodeInvalidConfig         ErrorCode = "CODE_NODE_INVALID_CONFIG"
	CodeNodeFunctionNotFound      ErrorCode = "CODE_NODE_FUNCTION_NOT_FOUND"
	CodeNodeInputMissing          ErrorCode = "CODE_NODE_INPUT_MISSING"
	CodeNodeInputTypeMismatch     ErrorCode = "CODE_NODE_INPUT_TYPE_MISMATCH"
	CodeNodeExecTimeout           ErrorCode = "CODE_NODE_EXEC_TIMEOUT"
	CodeNodeExecFailed            ErrorCode = "CODE_NODE_EXEC_FAILED"
	CodeNodeOutputMissing         ErrorCode = "CODE_NODE_OUTPUT_MISSING"
	CodeNodeOutputTypeMismatch    ErrorCode = "CODE_NODE_OUTPUT_TYPE_MISMATCH"
	CodeNodeOutputSchemaViolation ErrorCode = "CODE_NODE_OUTPUT_SCHEMA_VIOLATION"
)

type CodeNodeError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *CodeNodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func NewError(code ErrorCode, message string, cause error) error {
	return &CodeNodeError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
