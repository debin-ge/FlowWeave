package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	asrprovider "flowweave/internal/adapter/provider/asr"
)

const AsyncProviderName = "azure_batch"

type BatchConfig struct {
	Endpoint        string
	Region          string
	SubscriptionKey string
	APIVersion      string
	DefaultLocale   string
	HTTPTimeoutMS   int
}

type BatchProvider struct {
	endpoint        string
	apiVersion      string
	subscriptionKey string
	defaultLocale   string
	client          *http.Client
}

func NewBatchProvider(cfg BatchConfig) (*BatchProvider, error) {
	key := strings.TrimSpace(cfg.SubscriptionKey)
	if key == "" {
		return nil, fmt.Errorf("missing Azure Speech subscription key")
	}

	base := strings.TrimSpace(cfg.Endpoint)
	if base == "" {
		region := strings.TrimSpace(cfg.Region)
		if region == "" {
			return nil, fmt.Errorf("missing Azure Speech endpoint or region")
		}
		base = fmt.Sprintf("https://%s.api.cognitive.microsoft.com", region)
	}
	base = strings.TrimRight(base, "/")

	apiVersion := strings.TrimSpace(cfg.APIVersion)
	if apiVersion == "" {
		apiVersion = "2024-11-15"
	}
	locale := strings.TrimSpace(cfg.DefaultLocale)
	if locale == "" {
		locale = "zh-CN"
	}
	timeoutMS := cfg.HTTPTimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}

	return &BatchProvider{
		endpoint:        base,
		apiVersion:      apiVersion,
		subscriptionKey: key,
		defaultLocale:   locale,
		client:          &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond},
	}, nil
}

func (p *BatchProvider) Name() string {
	return AsyncProviderName
}

func (p *BatchProvider) SubmitTask(ctx context.Context, req *asrprovider.ASRAsyncSubmitRequest) (*asrprovider.ASRAsyncSubmitResult, error) {
	if req == nil {
		return nil, fmt.Errorf("submit request is nil")
	}
	if req.SourceType != asrprovider.ASRAudioSourceTypeURL || strings.TrimSpace(req.AudioURL) == "" {
		return nil, fmt.Errorf("azure batch transcription currently supports audio_url only")
	}

	body := map[string]interface{}{
		"displayName": p.optionString(req.Options, "display_name", fmt.Sprintf("flowweave-asr-%d", time.Now().Unix())),
		"locale":      p.optionString(req.Options, "locale", p.defaultLocale),
		"contentUrls": []string{strings.TrimSpace(req.AudioURL)},
	}
	if desc := p.optionString(req.Options, "description", ""); desc != "" {
		body["description"] = desc
	}
	if req.CallbackURL != "" {
		body["callbackUrl"] = req.CallbackURL
	}

	props := map[string]interface{}{}
	p.putOptBool(props, req.Options, "word_level_timestamps_enabled", "wordLevelTimestampsEnabled")
	p.putOptBool(props, req.Options, "display_form_word_level_timestamps_enabled", "displayFormWordLevelTimestampsEnabled")
	p.putOptBool(props, req.Options, "diarization_enabled", "diarizationEnabled")
	p.putOptString(props, req.Options, "punctuation_mode", "punctuationMode")
	p.putOptString(props, req.Options, "profanity_filter_mode", "profanityFilterMode")
	p.putOptInt(props, req.Options, "time_to_live_hours", "timeToLiveHours")
	if len(props) > 0 {
		body["properties"] = props
	}

	respBody, headers, err := p.doJSON(ctx, http.MethodPost, p.transcriptionsPath(), body, true)
	if err != nil {
		return nil, fmt.Errorf("create azure batch transcription failed: %w", err)
	}

	var created map[string]interface{}
	_ = json.Unmarshal(respBody, &created)

	taskID := strings.TrimSpace(p.readString(created, "id"))
	if taskID == "" {
		taskID = extractIDFromSelfURL(p.readString(created, "self"))
	}
	if taskID == "" {
		return nil, fmt.Errorf("azure create transcription returned empty task id")
	}

	return &asrprovider.ASRAsyncSubmitResult{
		ProviderTaskRef: taskID,
		RequestID:       firstNonEmpty(headers.Get("x-requestid"), headers.Get("X-RequestId")),
		Status:          mapAzureStatusToUnified(firstNonEmpty(p.readString(created, "status"), "NotStarted")),
		Raw:             created,
	}, nil
}

func (p *BatchProvider) QueryTask(ctx context.Context, providerTaskRef string) (*asrprovider.ASRAsyncQueryResult, error) {
	taskID := strings.TrimSpace(providerTaskRef)
	if taskID == "" {
		return nil, fmt.Errorf("provider_task_ref is empty")
	}

	respBody, headers, err := p.doJSON(ctx, http.MethodGet, p.transcriptionByIDPath(taskID), nil, false)
	if err != nil {
		return nil, fmt.Errorf("get azure transcription status failed: %w", err)
	}
	var task map[string]interface{}
	if err := json.Unmarshal(respBody, &task); err != nil {
		return nil, fmt.Errorf("decode azure transcription status failed: %w", err)
	}

	rawStatus := p.readString(task, "status")
	status := mapAzureStatusToUnified(rawStatus)
	result := &asrprovider.ASRAsyncQueryResult{
		ProviderTaskRef: taskID,
		Status:          status,
		RequestID:       firstNonEmpty(headers.Get("x-requestid"), headers.Get("X-RequestId")),
		Raw:             task,
	}

	if status == "failed" {
		errCode, errMsg := p.readAzureError(task)
		result.ErrorCode = firstNonEmpty(errCode, "AZURE_BATCH_FAILED")
		result.ErrorMessage = firstNonEmpty(errMsg, "azure batch transcription failed")
		return result, nil
	}
	if status != "succeeded" {
		return result, nil
	}

	filesURL := p.readNestedString(task, "links", "files")
	if filesURL == "" {
		filesURL = p.transcriptionFilesPath(taskID)
	}
	text, segments, rawFiles, fetchErr := p.fetchTranscriptionResult(ctx, filesURL)
	if fetchErr != nil {
		result.Status = "failed"
		result.ErrorCode = "AZURE_BATCH_RESULT_FETCH_FAILED"
		result.ErrorMessage = fetchErr.Error()
		return result, nil
	}
	result.Text = text
	result.Segments = segments
	if result.Raw == nil {
		result.Raw = map[string]interface{}{}
	}
	result.Raw["files"] = rawFiles
	return result, nil
}

func (p *BatchProvider) fetchTranscriptionResult(ctx context.Context, filesURL string) (string, []asrprovider.ASRSegment, map[string]interface{}, error) {
	filesBody, _, err := p.doJSON(ctx, http.MethodGet, filesURL, nil, false)
	if err != nil {
		return "", nil, nil, err
	}
	var files map[string]interface{}
	if err := json.Unmarshal(filesBody, &files); err != nil {
		return "", nil, nil, fmt.Errorf("decode files response failed: %w", err)
	}

	transcriptionURL := ""
	values, _ := files["values"].([]interface{})
	for _, item := range values {
		m, _ := item.(map[string]interface{})
		if strings.EqualFold(strings.TrimSpace(asString(m["kind"])), "Transcription") {
			if links, ok := m["links"].(map[string]interface{}); ok {
				transcriptionURL = strings.TrimSpace(asString(links["contentUrl"]))
				if transcriptionURL != "" {
					break
				}
			}
		}
	}
	if transcriptionURL == "" {
		return "", nil, files, fmt.Errorf("no transcription contentUrl found")
	}

	contentBody, _, err := p.doJSON(ctx, http.MethodGet, transcriptionURL, nil, false)
	if err != nil {
		return "", nil, files, err
	}
	var content map[string]interface{}
	if err := json.Unmarshal(contentBody, &content); err != nil {
		return "", nil, files, fmt.Errorf("decode transcription content failed: %w", err)
	}

	text := extractAzureText(content)
	segments := extractAzureSegments(content)
	return text, segments, files, nil
}

func (p *BatchProvider) doJSON(ctx context.Context, method, pathOrURL string, body interface{}, expectCreated bool) ([]byte, http.Header, error) {
	endpoint, err := p.buildURL(pathOrURL)
	if err != nil {
		return nil, nil, err
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", p.subscriptionKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if expectCreated {
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.Header, nil
}

func (p *BatchProvider) buildURL(pathOrURL string) (string, error) {
	raw := strings.TrimSpace(pathOrURL)
	if raw == "" {
		return "", fmt.Errorf("empty request path")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return appendAPIVersion(raw, p.apiVersion), nil
	}
	return appendAPIVersion(p.endpoint+raw, p.apiVersion), nil
}

func (p *BatchProvider) transcriptionsPath() string {
	return "/speechtotext/transcriptions"
}

func (p *BatchProvider) transcriptionByIDPath(taskID string) string {
	return "/speechtotext/transcriptions/" + url.PathEscape(taskID)
}

func (p *BatchProvider) transcriptionFilesPath(taskID string) string {
	return "/speechtotext/transcriptions/" + url.PathEscape(taskID) + "/files"
}

func appendAPIVersion(rawURL, version string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if q.Get("api-version") == "" && version != "" {
		q.Set("api-version", version)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func mapAzureStatusToUnified(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "notstarted":
		return "submitted"
	case "running":
		return "running"
	case "succeeded":
		return "succeeded"
	case "failed":
		return "failed"
	default:
		return "running"
	}
}

func extractAzureText(content map[string]interface{}) string {
	if content == nil {
		return ""
	}
	if arr, ok := content["combinedRecognizedPhrases"].([]interface{}); ok {
		lines := make([]string, 0, len(arr))
		for _, item := range arr {
			m, _ := item.(map[string]interface{})
			if line := firstNonEmpty(asString(m["display"]), asString(m["lexical"])); line != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	if arr, ok := content["recognizedPhrases"].([]interface{}); ok {
		lines := make([]string, 0, len(arr))
		for _, item := range arr {
			m, _ := item.(map[string]interface{})
			line := ""
			if nb, ok := m["nBest"].([]interface{}); ok && len(nb) > 0 {
				if n0, ok := nb[0].(map[string]interface{}); ok {
					line = firstNonEmpty(asString(n0["display"]), asString(n0["lexical"]))
				}
			}
			if line != "" {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func extractAzureSegments(content map[string]interface{}) []asrprovider.ASRSegment {
	if content == nil {
		return nil
	}
	arr, ok := content["recognizedPhrases"].([]interface{})
	if !ok {
		return nil
	}
	segments := make([]asrprovider.ASRSegment, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]interface{})
		seg := asrprovider.ASRSegment{
			StartMs: ticksToMS(m["offsetInTicks"]),
			EndMs:   ticksToMS(m["offsetInTicks"]) + ticksToMS(m["durationInTicks"]),
		}
		if nb, ok := m["nBest"].([]interface{}); ok && len(nb) > 0 {
			if n0, ok := nb[0].(map[string]interface{}); ok {
				seg.Text = firstNonEmpty(asString(n0["display"]), asString(n0["lexical"]))
			}
		}
		if seg.Text == "" {
			continue
		}
		segments = append(segments, seg)
	}
	return segments
}

func ticksToMS(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x / 10000.0)
	case int64:
		return x / 10000
	case int:
		return int64(x) / 10000
	case string:
		if x == "" {
			return 0
		}
		f, err := strconv.ParseFloat(x, 64)
		if err == nil {
			return int64(f / 10000.0)
		}
	}
	return 0
}

func (p *BatchProvider) readAzureError(task map[string]interface{}) (string, string) {
	props, _ := task["properties"].(map[string]interface{})
	if props == nil {
		return "", ""
	}
	errObj, _ := props["error"].(map[string]interface{})
	if errObj == nil {
		return "", ""
	}
	return asString(errObj["code"]), asString(errObj["message"])
}

func (p *BatchProvider) readString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	return asString(m[key])
}

func (p *BatchProvider) readNestedString(m map[string]interface{}, outer, inner string) string {
	outerMap, _ := m[outer].(map[string]interface{})
	return asString(outerMap[inner])
}

func extractIDFromSelfURL(selfURL string) string {
	u, err := url.Parse(strings.TrimSpace(selfURL))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func (p *BatchProvider) optionString(options map[string]interface{}, key, def string) string {
	if options == nil {
		return def
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (p *BatchProvider) putOptString(dst, options map[string]interface{}, inKey, outKey string) {
	if dst == nil || options == nil {
		return
	}
	if v, ok := options[inKey].(string); ok && strings.TrimSpace(v) != "" {
		dst[outKey] = strings.TrimSpace(v)
	}
}

func (p *BatchProvider) putOptBool(dst, options map[string]interface{}, inKey, outKey string) {
	if dst == nil || options == nil {
		return
	}
	switch v := options[inKey].(type) {
	case bool:
		dst[outKey] = v
	case string:
		b, err := strconv.ParseBool(v)
		if err == nil {
			dst[outKey] = b
		}
	}
}

func (p *BatchProvider) putOptInt(dst, options map[string]interface{}, inKey, outKey string) {
	if dst == nil || options == nil {
		return
	}
	switch v := options[inKey].(type) {
	case int:
		dst[outKey] = v
	case int64:
		dst[outKey] = int(v)
	case float64:
		dst[outKey] = int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			dst[outKey] = n
		}
	}
}

func asString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
