package azuretranslate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAzureTranslateExecuteSuccess(t *testing.T) {
	t.Setenv("AZURE_TRANSLATOR_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Path; got != "/translate" {
			t.Fatalf("unexpected path: %s", got)
		}
		if got := r.URL.Query().Get("api-version"); got != "3.0" {
			t.Fatalf("unexpected api-version: %s", got)
		}
		if got := r.URL.Query().Get("from"); got != "en" {
			t.Fatalf("unexpected from: %s", got)
		}
		if got := r.URL.Query()["to"]; len(got) != 2 || got[0] != "zh-Hans" || got[1] != "ja" {
			t.Fatalf("unexpected to: %v", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-key" {
			t.Fatalf("unexpected key header: %s", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Region"); got != "eastasia" {
			t.Fatalf("unexpected region header: %s", got)
		}

		var body []map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if len(body) != 2 || body[0]["Text"] != "hello" || body[1]["Text"] != "world" {
			t.Fatalf("unexpected body: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"detectedLanguage":{"language":"en","score":1},"translations":[{"text":"你好","to":"zh-Hans"},{"text":"こんにちは","to":"ja"}]},
			{"translations":[{"text":"世界","to":"zh-Hans"},{"text":"世界","to":"ja"}]}
		]`))
	}))
	defer server.Close()

	fn := &function{httpClient: server.Client()}
	out, err := fn.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"endpoint": server.URL,
			"region":   "eastasia",
			"from":     "en",
			"to":       []interface{}{"zh-Hans", "ja"},
			"texts":    []interface{}{"hello", "world"},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if out["translated_text"] != "你好" {
		t.Fatalf("unexpected translated_text: %#v", out["translated_text"])
	}
	if out["translation_count"] != 2 {
		t.Fatalf("unexpected translation_count: %#v", out["translation_count"])
	}
	if out["request_character_count"] != 10 {
		t.Fatalf("unexpected request_character_count: %#v", out["request_character_count"])
	}
}

func TestAzureTranslateExecuteMissingKey(t *testing.T) {
	fn := &function{}
	_, err := fn.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"text": "hello",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAzureTranslateExecuteHTTPError(t *testing.T) {
	t.Setenv("AZURE_TRANSLATOR_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	fn := &function{httpClient: server.Client()}
	_, err := fn.Execute(context.Background(), map[string]interface{}{
		"args": map[string]interface{}{
			"endpoint": server.URL,
			"text":     "hello",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
