package tencent

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tcasr "github.com/tencentcloud/tencentcloud-speech-sdk-go/asr"
	"github.com/tencentcloud/tencentcloud-speech-sdk-go/common"

	asrprovider "flowweave/internal/adapter/provider/asr"
)

const ProviderName = "tencent_flash"

// Config holds Tencent Flash credentials and defaults.
type Config struct {
	AppID             string
	SecretID          string
	SecretKey         string
	DefaultEngineType string
}

// FlashProvider wraps Tencent FlashRecognizer.
type FlashProvider struct {
	recognizer        *tcasr.FlashRecognizer
	defaultEngineType string
}

func NewFlashProvider(cfg Config) (*FlashProvider, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("missing Tencent ASR credentials")
	}
	engine := strings.TrimSpace(cfg.DefaultEngineType)
	if engine == "" {
		engine = "16k_zh"
	}
	cred := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	return &FlashProvider{
		recognizer:        tcasr.NewFlashRecognizer(cfg.AppID, cred),
		defaultEngineType: engine,
	}, nil
}

func (p *FlashProvider) Name() string {
	return ProviderName
}

func (p *FlashProvider) Transcribe(ctx context.Context, req *asrprovider.ASRTranscribeRequest) (*asrprovider.ASRTranscribeResult, error) {
	if req == nil || len(req.AudioBytes) == 0 {
		return nil, fmt.Errorf("empty audio bytes")
	}

	flashReq := &tcasr.FlashRecognitionRequest{
		EngineType:         p.optionString(req.Options, "engine_type", p.defaultEngineType),
		VoiceFormat:        detectVoiceFormat(req),
		SpeakerDiarization: uint32(p.optionInt(req.Options, "speaker_diarization", 0)),
		HotwordId:          p.optionString(req.Options, "hotword_id", ""),
		HotwordList:        buildHotwordList(req.Options),
		CustomizationId:    p.optionString(req.Options, "customization_id", ""),
		FilterDirty:        int32(p.optionInt(req.Options, "filter_dirty", 0)),
		FilterModal:        int32(p.optionInt(req.Options, "filter_modal", 0)),
		FilterPunc:         int32(p.optionInt(req.Options, "filter_punc", 0)),
		ConvertNumMode:     int32(p.optionInt(req.Options, "convert_num_mode", 1)),
		WordInfo:           int32(p.optionInt(req.Options, "word_info", 0)),
		FirstChannelOnly:   int32(p.optionInt(req.Options, "first_channel_only", 1)),
		ReinforceHotword:   int32(p.optionInt(req.Options, "reinforce_hotword", 0)),
		SentenceMaxLength:  int32(p.optionInt(req.Options, "sentence_max_length", 0)),
	}

	start := time.Now()
	resp, err := p.recognizer.Recognize(flashReq, req.AudioBytes)
	if err != nil {
		return nil, fmt.Errorf("tencent flash recognize failed: %w", err)
	}

	segments := make([]asrprovider.ASRSegment, 0)
	texts := make([]string, 0, len(resp.FlashResult))
	for _, ch := range resp.FlashResult {
		if strings.TrimSpace(ch.Text) != "" {
			texts = append(texts, ch.Text)
		}
		for _, sentence := range ch.SentenceList {
			segments = append(segments, asrprovider.ASRSegment{
				Text:    sentence.Text,
				StartMs: int64(sentence.StartTime),
				EndMs:   int64(sentence.EndTime),
				Speaker: strconv.FormatInt(int64(sentence.SpeakerId), 10),
			})
		}
	}

	return &asrprovider.ASRTranscribeResult{
		Text:      strings.Join(texts, "\n"),
		Segments:  segments,
		RequestID: resp.RequestId,
		Provider:  p.Name(),
		ElapsedMs: time.Since(start).Milliseconds(),
		Raw: map[string]interface{}{
			"audio_duration": resp.AudioDuration,
			"code":           resp.Code,
			"message":        resp.Message,
		},
	}, nil
}

func detectVoiceFormat(req *asrprovider.ASRTranscribeRequest) string {
	if req != nil {
		if s, ok := req.Options["voice_format"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.ToLower(strings.TrimSpace(s))
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(req.Filename), "."))
		switch ext {
		case "pcm", "wav", "mp3", "m4a", "aac", "ogg", "speex", "silk", "flac", "amr":
			return ext
		}
	}
	return "mp3"
}

func buildHotwordList(options map[string]interface{}) string {
	if options == nil {
		return ""
	}
	if s, ok := options["hotword_list"].(string); ok {
		return s
	}
	if arr, ok := options["hotword_list"].([]interface{}); ok {
		parts := make([]string, 0, len(arr))
		for _, item := range arr {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				parts = append(parts, strings.TrimSpace(str))
			}
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func (p *FlashProvider) optionString(options map[string]interface{}, key, def string) string {
	if options == nil {
		return def
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (p *FlashProvider) optionInt(options map[string]interface{}, key string, def int) int {
	if options == nil {
		return def
	}
	v, ok := options[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}
