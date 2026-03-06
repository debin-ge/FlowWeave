package asr

import "context"

// ASRAsyncSubmitRequest describes provider-agnostic async submit request.
type ASRAsyncSubmitRequest struct {
	SourceType    ASRAudioSourceType     `json:"source_type"`
	AudioURL      string                 `json:"audio_url,omitempty"`
	AudioBase64   string                 `json:"audio_base64,omitempty"`
	Options       map[string]interface{} `json:"options,omitempty"`
	CallbackURL   string                 `json:"callback_url,omitempty"`
	CallbackToken string                 `json:"callback_token,omitempty"`
}

// ASRAsyncSubmitResult describes provider async task creation response.
type ASRAsyncSubmitResult struct {
	ProviderTaskRef string                 `json:"provider_task_ref"`
	RequestID       string                 `json:"request_id,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Raw             map[string]interface{} `json:"raw,omitempty"`
}

// ASRAsyncQueryResult describes provider async status query response.
type ASRAsyncQueryResult struct {
	ProviderTaskRef string                 `json:"provider_task_ref"`
	Status          string                 `json:"status"`
	Text            string                 `json:"text,omitempty"`
	Segments        []ASRSegment           `json:"segments,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	ErrorMessage    string                 `json:"error_message,omitempty"`
	RequestID       string                 `json:"request_id,omitempty"`
	Raw             map[string]interface{} `json:"raw,omitempty"`
}

// ASRAsyncProvider defines async submit/query capabilities.
type ASRAsyncProvider interface {
	Name() string
	SubmitTask(ctx context.Context, req *ASRAsyncSubmitRequest) (*ASRAsyncSubmitResult, error)
	QueryTask(ctx context.Context, providerTaskRef string) (*ASRAsyncQueryResult, error)
}

// ASRAsyncRuntimeConfig controls generic async behavior.
type ASRAsyncRuntimeConfig struct {
	CallbackBaseURL       string
	DefaultPollIntervalMS int
	DefaultWaitTimeoutMS  int
}

var asrAsyncRuntimeConfig = ASRAsyncRuntimeConfig{
	CallbackBaseURL:       "",
	DefaultPollIntervalMS: 1500,
	DefaultWaitTimeoutMS:  120000,
}

func SetASRAsyncRuntimeConfig(cfg ASRAsyncRuntimeConfig) {
	if cfg.CallbackBaseURL != "" {
		asrAsyncRuntimeConfig.CallbackBaseURL = cfg.CallbackBaseURL
	}
	if cfg.DefaultPollIntervalMS > 0 {
		asrAsyncRuntimeConfig.DefaultPollIntervalMS = cfg.DefaultPollIntervalMS
	}
	if cfg.DefaultWaitTimeoutMS > 0 {
		asrAsyncRuntimeConfig.DefaultWaitTimeoutMS = cfg.DefaultWaitTimeoutMS
	}
}

func GetASRAsyncRuntimeConfig() ASRAsyncRuntimeConfig {
	return asrAsyncRuntimeConfig
}
