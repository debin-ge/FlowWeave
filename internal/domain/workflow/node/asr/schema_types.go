package asr

import (
	"time"

	types "flowweave/internal/domain/workflow/model"
)

const (
	defaultASRTimeoutMS = 120000
)

// ASRNodeData is DSL payload for asr node.
type ASRNodeData struct {
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Provider    string                 `json:"provider"`
	AudioSource AudioSourceConfig      `json:"audio_source"`
	Options     map[string]interface{} `json:"options,omitempty"`
	TimeoutMS   int                    `json:"timeout_ms,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
}

type AudioSourceConfig struct {
	Type          string                 `json:"type"`
	ValueSelector types.VariableSelector `json:"value_selector"`
}

func (d *ASRNodeData) GetTimeout() time.Duration {
	ms := defaultASRTimeoutMS
	if d != nil && d.TimeoutMS > 0 {
		ms = d.TimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

func (d *ASRNodeData) GetMaxRetries() int {
	if d != nil && d.MaxRetries > 0 {
		return d.MaxRetries
	}
	return 1
}
