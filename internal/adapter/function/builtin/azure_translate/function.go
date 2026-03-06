package azuretranslate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"flowweave/internal/domain/workflow/node/code"
)

const (
	defaultEndpoint      = "https://api.cognitive.microsofttranslator.com"
	defaultAPIVersion    = "3.0"
	defaultTimeoutMS     = 30000
	defaultTextType      = "plain"
	defaultOutputKey     = "translated_text"
	translateFunctionRef = "azure.translate.v1"
)

type function struct {
	httpClient *http.Client
}

type requestItem struct {
	Text string `json:"Text"`
}

type responseItem struct {
	DetectedLanguage map[string]interface{} `json:"detectedLanguage"`
	Translations     []translationItem      `json:"translations"`
}

type translationItem struct {
	Text      string `json:"text"`
	To        string `json:"to"`
	Translit  string `json:"transliteration,omitempty"`
	Alignment struct {
		Proj string `json:"proj"`
	} `json:"alignment,omitempty"`
	SentenceInfo struct {
		SrcSentLen   []int `json:"srcSentLen,omitempty"`
		TransSentLen []int `json:"transSentLen,omitempty"`
	} `json:"sentLen,omitempty"`
}

func (f *function) Name() string {
	return translateFunctionRef
}

func (f *function) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	args, err := parseArgs(input)
	if err != nil {
		return nil, err
	}

	cfg, err := loadRuntimeConfig(args)
	if err != nil {
		return nil, err
	}

	texts, err := getTexts(args)
	if err != nil {
		return nil, err
	}

	endpoint, err := buildTranslateURL(cfg, args)
	if err != nil {
		return nil, err
	}

	body := make([]requestItem, 0, len(texts))
	for _, text := range texts {
		body = append(body, requestItem{Text: text})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal azure translate request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build azure translate request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ocp-Apim-Subscription-Key", cfg.SubscriptionKey)
	if cfg.Region != "" {
		req.Header.Set("Ocp-Apim-Subscription-Region", cfg.Region)
	}
	if traceID := getOptionalString(args, "trace_id"); traceID != "" {
		req.Header.Set("X-ClientTraceId", traceID)
	}

	resp, err := f.client(cfg.TimeoutMS).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call azure translate failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("azure translate returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded []responseItem
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode azure translate response failed: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("azure translate response is empty")
	}

	translations := make([]map[string]interface{}, 0, len(decoded))
	for _, item := range decoded {
		out := map[string]interface{}{
			"translations": flattenTranslations(item.Translations),
		}
		if len(item.Translations) > 0 {
			out["translated_text"] = item.Translations[0].Text
			out["to"] = item.Translations[0].To
		}
		if len(item.DetectedLanguage) > 0 {
			out["detected_language"] = item.DetectedLanguage
		}
		translations = append(translations, out)
	}

	first := translations[0]
	result := map[string]interface{}{
		defaultOutputKey:          first["translated_text"],
		"to":                      first["to"],
		"translations":            translations,
		"translation_count":       len(translations),
		"target_languages":        getToLanguages(args),
		"request_character_count": countCharacters(texts),
	}
	if detected, ok := first["detected_language"]; ok {
		result["detected_language"] = detected
	}
	if from := getOptionalString(args, "from"); from != "" {
		result["from"] = from
	}

	return result, nil
}

type runtimeConfig struct {
	Endpoint        string
	SubscriptionKey string
	Region          string
	APIVersion      string
	TimeoutMS       int
}

func loadRuntimeConfig(args map[string]interface{}) (*runtimeConfig, error) {
	cfg := &runtimeConfig{
		Endpoint:        firstNonEmpty(getOptionalString(args, "endpoint"), os.Getenv("AZURE_TRANSLATOR_ENDPOINT"), defaultEndpoint),
		SubscriptionKey: firstNonEmpty(getOptionalString(args, "subscription_key"), os.Getenv("AZURE_TRANSLATOR_KEY")),
		Region:          firstNonEmpty(getOptionalString(args, "region"), os.Getenv("AZURE_TRANSLATOR_REGION")),
		APIVersion:      firstNonEmpty(getOptionalString(args, "api_version"), os.Getenv("AZURE_TRANSLATOR_API_VERSION"), defaultAPIVersion),
		TimeoutMS:       getOptionalInt(args, "http_timeout_ms", envInt("AZURE_TRANSLATOR_HTTP_TIMEOUT_MS", defaultTimeoutMS)),
	}

	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.SubscriptionKey = strings.TrimSpace(cfg.SubscriptionKey)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.APIVersion = strings.TrimSpace(cfg.APIVersion)

	if cfg.SubscriptionKey == "" {
		return nil, fmt.Errorf("missing Azure Translator subscription key: set args.subscription_key or AZURE_TRANSLATOR_KEY")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("missing Azure Translator endpoint")
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultTimeoutMS
	}
	return cfg, nil
}

func buildTranslateURL(cfg *runtimeConfig, args map[string]interface{}) (string, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid azure translator endpoint: %w", err)
	}
	if !strings.HasSuffix(u.Path, "/translate") {
		u.Path = strings.TrimRight(u.Path, "/") + "/translate"
	}
	q := u.Query()
	q.Set("api-version", cfg.APIVersion)

	if from := getOptionalString(args, "from"); from != "" {
		q.Set("from", from)
	}
	for _, to := range getToLanguages(args) {
		q.Add("to", to)
	}
	textType := firstNonEmpty(getOptionalString(args, "text_type"), defaultTextType)
	q.Set("textType", textType)

	if category := getOptionalString(args, "category"); category != "" {
		q.Set("category", category)
	}
	if profanityAction := getOptionalString(args, "profanity_action"); profanityAction != "" {
		q.Set("profanityAction", profanityAction)
	}
	if profanityMarker := getOptionalString(args, "profanity_marker"); profanityMarker != "" {
		q.Set("profanityMarker", profanityMarker)
	}
	if includeAlignment, ok := getOptionalBool(args, "include_alignment"); ok {
		q.Set("includeAlignment", fmt.Sprintf("%t", includeAlignment))
	}
	if includeSentenceLength, ok := getOptionalBool(args, "include_sentence_length"); ok {
		q.Set("includeSentenceLength", fmt.Sprintf("%t", includeSentenceLength))
	}
	if suggestedFrom := getOptionalString(args, "suggested_from"); suggestedFrom != "" {
		q.Set("suggestedFrom", suggestedFrom)
	}
	if fromScript := getOptionalString(args, "from_script"); fromScript != "" {
		q.Set("fromScript", fromScript)
	}
	if toScript := getOptionalString(args, "to_script"); toScript != "" {
		q.Set("toScript", toScript)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func parseArgs(input map[string]interface{}) (map[string]interface{}, error) {
	if raw, ok := input["args"]; ok {
		args, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("input args must be object, got %T", raw)
		}
		return args, nil
	}
	return input, nil
}

func getTexts(args map[string]interface{}) ([]string, error) {
	if text := strings.TrimSpace(getOptionalString(args, "text")); text != "" {
		return []string{text}, nil
	}
	if rawTexts, ok := args["texts"]; ok {
		items, ok := rawTexts.([]interface{})
		if !ok {
			return nil, fmt.Errorf("args.texts must be array, got %T", rawTexts)
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("args.texts must contain only strings, got %T", item)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return nil, fmt.Errorf("args.texts must not contain empty strings")
			}
			out = append(out, text)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("missing required args field: text or texts")
}

func getToLanguages(args map[string]interface{}) []string {
	if raw, ok := args["to"]; ok {
		switch v := raw.(type) {
		case string:
			val := strings.TrimSpace(v)
			if val != "" {
				return []string{val}
			}
		case []interface{}:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []string{"zh-Hans"}
}

func flattenTranslations(items []translationItem) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		entry := map[string]interface{}{
			"text": item.Text,
			"to":   item.To,
		}
		if item.Translit != "" {
			entry["transliteration"] = item.Translit
		}
		if item.Alignment.Proj != "" {
			entry["alignment"] = map[string]interface{}{"proj": item.Alignment.Proj}
		}
		if len(item.SentenceInfo.SrcSentLen) > 0 || len(item.SentenceInfo.TransSentLen) > 0 {
			entry["sent_len"] = map[string]interface{}{
				"src_sent_len":   item.SentenceInfo.SrcSentLen,
				"trans_sent_len": item.SentenceInfo.TransSentLen,
			}
		}
		out = append(out, entry)
	}
	return out
}

func getOptionalString(args map[string]interface{}, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	val, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(val)
}

func getOptionalInt(args map[string]interface{}, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func getOptionalBool(args map[string]interface{}, key string) (bool, bool) {
	raw, ok := args[key]
	if !ok {
		return false, false
	}
	val, ok := raw.(bool)
	return val, ok
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return def
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func countCharacters(items []string) int {
	total := 0
	for _, item := range items {
		total += len([]rune(item))
	}
	return total
}

func (f *function) client(timeoutMS int) *http.Client {
	if f.httpClient != nil {
		return f.httpClient
	}
	return &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
}

func init() {
	code.MustRegisterFunction(&function{})
}
