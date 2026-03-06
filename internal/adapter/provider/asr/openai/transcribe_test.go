package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	asrprovider "flowweave/internal/adapter/provider/asr"
)

func TestTranscribeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/audio/transcriptions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Fatalf("unexpected auth: %s", auth)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart failed: %v", err)
		}
		if got := r.FormValue("model"); got != "gpt-4o-transcribe" {
			t.Fatalf("unexpected model: %s", got)
		}
		if got := r.FormValue("language"); got != "zh" {
			t.Fatalf("unexpected language: %s", got)
		}
		if got := r.FormValue("response_format"); got != "verbose_json" {
			t.Fatalf("unexpected response_format: %s", got)
		}
		if got := r.Form["timestamp_granularities[]"]; len(got) != 2 || got[0] != "segment" || got[1] != "word" {
			t.Fatalf("unexpected timestamp_granularities: %#v", got)
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing file form-data: %v", err)
		}
		defer file.Close()
		payload, _ := io.ReadAll(file)
		if string(payload) != "audio-data" {
			t.Fatalf("unexpected file payload: %s", string(payload))
		}

		w.Header().Set("x-request-id", "req-1")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"text": "你好世界",
			"segments": []map[string]interface{}{
				{"text": "你好", "start": 0.0, "end": 0.6},
				{"text": "世界", "start": 0.6, "end": 1.2},
			},
		})
	}))
	defer server.Close()

	p, err := NewProvider(Config{
		APIKey:       "test-key",
		BaseURL:      server.URL,
		DefaultModel: "gpt-4o-transcribe",
		HTTPTimeout:  2000,
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	result, err := p.Transcribe(context.Background(), &asrprovider.ASRTranscribeRequest{
		AudioBytes: []byte("audio-data"),
		Filename:   "sample.wav",
		Options: map[string]interface{}{
			"language":                "zh",
			"response_format":         "verbose_json",
			"timestamp_granularities": []interface{}{"segment", "word"},
		},
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if result.Provider != ProviderName {
		t.Fatalf("unexpected provider: %s", result.Provider)
	}
	if result.RequestID != "req-1" {
		t.Fatalf("unexpected request id: %s", result.RequestID)
	}
	if result.Text != "你好世界" {
		t.Fatalf("unexpected text: %s", result.Text)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("unexpected segments len: %d", len(result.Segments))
	}
	if result.Segments[0].StartMs != 0 || result.Segments[0].EndMs != 600 {
		t.Fatalf("unexpected segment timing: %+v", result.Segments[0])
	}
}

func TestTranscribeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	p, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	_, err = p.Transcribe(context.Background(), &asrprovider.ASRTranscribeRequest{
		AudioBytes: []byte("audio-data"),
	})
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("expected status 400 error, got: %v", err)
	}
}

func TestTranscribeTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain-text-result"))
	}))
	defer server.Close()

	p, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	result, err := p.Transcribe(context.Background(), &asrprovider.ASRTranscribeRequest{
		AudioBytes: []byte("audio-data"),
	})
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}
	if result.Text != "plain-text-result" {
		t.Fatalf("unexpected text: %s", result.Text)
	}
}
