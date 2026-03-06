package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	asrprovider "flowweave/internal/adapter/provider/asr"
)

const ProviderName = "openai_asr"

type Config struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	HTTPTimeout  int
}

type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
}

type transcriptionResponse struct {
	Text     string        `json:"text"`
	Segments []segmentItem `json:"segments,omitempty"`
}

type segmentItem struct {
	Text  string  `json:"text"`
	Start float64 `json:"start,omitempty"`
	End   float64 `json:"end,omitempty"`
}

func NewProvider(cfg Config) (*Provider, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("missing OpenAI ASR API key")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	model := strings.TrimSpace(cfg.DefaultModel)
	if model == "" {
		model = "gpt-4o-transcribe"
	}

	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 30000
	}

	return &Provider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		defaultModel: model,
		client:       &http.Client{Timeout: time.Duration(timeout) * time.Millisecond},
	}, nil
}

func (p *Provider) Name() string {
	return ProviderName
}

func (p *Provider) Transcribe(ctx context.Context, req *asrprovider.ASRTranscribeRequest) (*asrprovider.ASRTranscribeResult, error) {
	if req == nil || len(req.AudioBytes) == 0 {
		return nil, fmt.Errorf("empty audio bytes")
	}

	body, contentType, err := p.buildMultipartBody(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/audio/transcriptions", body)
	if err != nil {
		return nil, fmt.Errorf("build OpenAI ASR request failed: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", contentType)

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call OpenAI ASR failed: %w", err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenAI ASR returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	text, segments, raw, err := parseResponse(payload)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI ASR response failed: %w", err)
	}

	return &asrprovider.ASRTranscribeResult{
		Text:      text,
		Segments:  segments,
		RequestID: firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("X-Request-Id")),
		Provider:  p.Name(),
		ElapsedMs: time.Since(start).Milliseconds(),
		Raw:       raw,
	}, nil
}

func (p *Provider) buildMultipartBody(req *asrprovider.ASRTranscribeRequest) (*bytes.Buffer, string, error) {
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)

	model := p.optionString(req.Options, "model", p.defaultModel)
	if err := writer.WriteField("model", model); err != nil {
		return nil, "", err
	}

	filename := sanitizeFilename(req.Filename)
	fileWriter, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := fileWriter.Write(req.AudioBytes); err != nil {
		return nil, "", err
	}

	p.putOptionalString(writer, req.Options, "language")
	p.putOptionalString(writer, req.Options, "prompt")
	p.putOptionalString(writer, req.Options, "response_format")
	p.putOptionalFloat(writer, req.Options, "temperature")

	if arr, ok := req.Options["timestamp_granularities"]; ok {
		items := asStringSlice(arr)
		for _, item := range items {
			if err := writer.WriteField("timestamp_granularities[]", item); err != nil {
				return nil, "", err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &b, writer.FormDataContentType(), nil
}

func parseResponse(payload []byte) (string, []asrprovider.ASRSegment, map[string]interface{}, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		text := strings.TrimSpace(string(payload))
		if text == "" {
			return "", nil, nil, fmt.Errorf("empty response payload")
		}
		return text, nil, map[string]interface{}{"text": text}, nil
	}

	var decoded transcriptionResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", nil, raw, err
	}

	segments := make([]asrprovider.ASRSegment, 0, len(decoded.Segments))
	for _, s := range decoded.Segments {
		segments = append(segments, asrprovider.ASRSegment{
			Text:    s.Text,
			StartMs: int64(s.Start * 1000),
			EndMs:   int64(s.End * 1000),
		})
	}

	text := strings.TrimSpace(decoded.Text)
	if text == "" {
		text = readString(raw, "text")
	}
	return text, segments, raw, nil
}

func (p *Provider) optionString(options map[string]interface{}, key, def string) string {
	if options == nil {
		return def
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (p *Provider) putOptionalString(writer *multipart.Writer, options map[string]interface{}, key string) {
	if options == nil {
		return
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		_ = writer.WriteField(key, strings.TrimSpace(v))
	}
}

func (p *Provider) putOptionalFloat(writer *multipart.Writer, options map[string]interface{}, key string) {
	if options == nil {
		return
	}
	raw, ok := options[key]
	if !ok || raw == nil {
		return
	}

	var value float64
	switch n := raw.(type) {
	case float64:
		value = n
	case float32:
		value = float64(n)
	case int:
		value = float64(n)
	case int32:
		value = float64(n)
	case int64:
		value = float64(n)
	default:
		return
	}
	_ = writer.WriteField(key, fmt.Sprintf("%g", value))
}

func asStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func sanitizeFilename(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "audio.wav"
	}
	base := filepath.Base(name)
	if strings.TrimSpace(base) == "" || base == "." || base == "/" {
		return "audio.wav"
	}
	return base
}

func readString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
