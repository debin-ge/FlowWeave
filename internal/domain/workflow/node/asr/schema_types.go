package asr

import (
	"time"

	asrprovider "flowweave/internal/adapter/provider/asr"
	types "flowweave/internal/domain/workflow/model"
)

const (
	defaultASRTimeoutMS = 120000
)

// ASRNodeData is DSL payload for asr node.
type ASRNodeData struct {
	Type           string                 `json:"type"`
	Title          string                 `json:"title"`
	Provider       string                 `json:"provider"`
	AsyncMode      string                 `json:"async_mode,omitempty"` // submit_only | submit_and_wait
	AudioSource    AudioSourceConfig      `json:"audio_source"`
	Options        map[string]interface{} `json:"options,omitempty"`
	TimeoutMS      int                    `json:"timeout_ms,omitempty"`
	MaxRetries     int                    `json:"max_retries,omitempty"`
	PollIntervalMS int                    `json:"poll_interval_ms,omitempty"`
	WaitTimeoutMS  int                    `json:"wait_timeout_ms,omitempty"`
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

func (d *ASRNodeData) IsAsyncMode() bool {
	if d == nil {
		return false
	}
	switch d.AsyncMode {
	case "submit_only", "submit_and_wait":
		return true
	default:
		return false
	}
}

func (d *ASRNodeData) GetPollInterval() time.Duration {
	ms := asrprovider.GetASRAsyncRuntimeConfig().DefaultPollIntervalMS
	if d != nil && d.PollIntervalMS > 0 {
		ms = d.PollIntervalMS
	}
	if ms <= 0 {
		ms = 1500
	}
	return time.Duration(ms) * time.Millisecond
}

func (d *ASRNodeData) GetWaitTimeout() time.Duration {
	ms := asrprovider.GetASRAsyncRuntimeConfig().DefaultWaitTimeoutMS
	if d != nil && d.WaitTimeoutMS > 0 {
		ms = d.WaitTimeoutMS
	}
	if ms <= 0 {
		ms = 120000
	}
	return time.Duration(ms) * time.Millisecond
}
