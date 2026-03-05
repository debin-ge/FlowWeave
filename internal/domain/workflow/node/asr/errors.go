package asr

import "fmt"

type ErrorCode string

const (
	ASRInvalidConfig      ErrorCode = "ASR_INVALID_CONFIG"
	ASRInvalidInput       ErrorCode = "ASR_INVALID_INPUT"
	ASRInputTooLarge      ErrorCode = "ASR_INPUT_TOO_LARGE"
	ASRBase64DecodeFailed ErrorCode = "ASR_BASE64_DECODE_FAILED"
	ASRFilePathInvalid    ErrorCode = "ASR_FILE_PATH_INVALID"
	ASRFileNotFound       ErrorCode = "ASR_FILE_NOT_FOUND"
	ASRURLBlocked         ErrorCode = "ASR_URL_BLOCKED"
	ASRURLFetchFailed     ErrorCode = "ASR_URL_FETCH_FAILED"
	ASRTimeout            ErrorCode = "ASR_TIMEOUT"
	ASRProviderCallFailed ErrorCode = "ASR_PROVIDER_CALL_FAILED"
	ASRConfigMissing      ErrorCode = "ASR_CONFIG_MISSING"
)

type ASRNodeError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *ASRNodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func newError(code ErrorCode, message string, cause error) error {
	return &ASRNodeError{Code: code, Message: message, Cause: cause}
}
