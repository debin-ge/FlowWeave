package asr

import (
	"strings"
)

func ValidateASRNodeData(data *ASRNodeData) error {
	if data == nil {
		return newError(ASRInvalidConfig, "asr node config is nil", nil)
	}
	if strings.TrimSpace(data.Provider) == "" {
		return newError(ASRInvalidConfig, "provider is required", nil)
	}
	if strings.TrimSpace(data.AudioSource.Type) == "" {
		return newError(ASRInvalidConfig, "audio_source.type is required", nil)
	}
	switch data.AudioSource.Type {
	case "audio_url", "audio_base64", "audio_file":
	default:
		return newError(ASRInvalidConfig, "unsupported audio_source.type: "+data.AudioSource.Type, nil)
	}
	if len(data.AudioSource.ValueSelector) != 2 {
		return newError(ASRInvalidConfig, "audio_source.value_selector must be [node_id,var_name]", nil)
	}
	if data.TimeoutMS < 0 {
		return newError(ASRInvalidConfig, "timeout_ms cannot be negative", nil)
	}
	if data.MaxRetries < 0 {
		return newError(ASRInvalidConfig, "max_retries cannot be negative", nil)
	}
	return nil
}
