package asr

import "context"

// ASRAudioSourceType indicates how audio is provided.
type ASRAudioSourceType string

const (
	ASRAudioSourceTypeURL    ASRAudioSourceType = "audio_url"
	ASRAudioSourceTypeBase64 ASRAudioSourceType = "audio_base64"
	ASRAudioSourceTypeFile   ASRAudioSourceType = "audio_file"
)

// ASRSegment is a sentence-level transcription segment.
type ASRSegment struct {
	Text    string `json:"text"`
	StartMs int64  `json:"start_ms,omitempty"`
	EndMs   int64  `json:"end_ms,omitempty"`
	Speaker string `json:"speaker,omitempty"`
}

// ASRTranscribeRequest is a normalized request for ASR providers.
type ASRTranscribeRequest struct {
	SourceType  ASRAudioSourceType     `json:"source_type"`
	AudioURL    string                 `json:"audio_url,omitempty"`
	AudioBase64 string                 `json:"audio_base64,omitempty"`
	AudioBytes  []byte                 `json:"-"`
	Filename    string                 `json:"filename,omitempty"`
	ContentType string                 `json:"content_type,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// ASRTranscribeResult is a normalized ASR response.
type ASRTranscribeResult struct {
	Text      string                 `json:"text"`
	Segments  []ASRSegment           `json:"segments,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Provider  string                 `json:"provider"`
	ElapsedMs int64                  `json:"elapsed_ms"`
	Raw       map[string]interface{} `json:"raw,omitempty"`
}

// ASRProvider defines sync transcription capability.
type ASRProvider interface {
	Name() string
	Transcribe(ctx context.Context, req *ASRTranscribeRequest) (*ASRTranscribeResult, error)
}

// ASRRuntimeConfig holds shared runtime limits and paths used by ASR-related modules.
type ASRRuntimeConfig struct {
	TempDir           string
	MaxAudioMB        int
	MaxBase64Chars    int
	URLFetchTimeoutMS int
}

var asrRuntimeConfig = ASRRuntimeConfig{
	TempDir:           "/tmp/flowweave-asr",
	MaxAudioMB:        50,
	MaxBase64Chars:    12 * 1024 * 1024,
	URLFetchTimeoutMS: 30000,
}

// SetASRRuntimeConfig updates global ASR runtime settings.
func SetASRRuntimeConfig(cfg ASRRuntimeConfig) {
	if cfg.TempDir != "" {
		asrRuntimeConfig.TempDir = cfg.TempDir
	}
	if cfg.MaxAudioMB > 0 {
		asrRuntimeConfig.MaxAudioMB = cfg.MaxAudioMB
	}
	if cfg.MaxBase64Chars > 0 {
		asrRuntimeConfig.MaxBase64Chars = cfg.MaxBase64Chars
	}
	if cfg.URLFetchTimeoutMS > 0 {
		asrRuntimeConfig.URLFetchTimeoutMS = cfg.URLFetchTimeoutMS
	}
}

// GetASRRuntimeConfig returns current ASR runtime settings.
func GetASRRuntimeConfig() ASRRuntimeConfig {
	return asrRuntimeConfig
}
