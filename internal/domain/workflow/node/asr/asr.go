package asr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	asrprovider "flowweave/internal/adapter/provider/asr"
	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
)

// ASRNode executes speech-to-text transcription.
type ASRNode struct {
	*node.BaseNode
	data ASRNodeData
}

func init() {
	node.Register(types.NodeTypeASR, NewASRNode)
}

func NewASRNode(id string, rawData json.RawMessage) (node.Node, error) {
	var data ASRNodeData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if err := ValidateASRNodeData(&data); err != nil {
		return nil, err
	}
	return &ASRNode{
		BaseNode: node.NewBaseNode(id, types.NodeTypeASR, data.Title, types.NodeExecutionTypeExecutable),
		data:     data,
	}, nil
}

func (n *ASRNode) Run(ctx context.Context) (<-chan event.NodeEvent, error) {
	return node.RunWithEvents(ctx, n, func(ctx context.Context) (*node.NodeRunResult, error) {
		vp, ok := node.GetVariablePoolFromContext(ctx)
		if !ok {
			return failResult(newError(ASRInvalidConfig, "variable pool not found in context", nil)), nil
		}

		rawSource, ok := vp.GetVariable(n.data.AudioSource.ValueSelector)
		if !ok {
			return failResult(newError(ASRInvalidInput, "audio source value not found by selector", nil)), nil
		}

		if n.data.IsAsyncMode() {
			return n.runAsync(ctx, rawSource)
		}
		return n.runSync(ctx, rawSource)
	})
}

func (n *ASRNode) runSync(ctx context.Context, rawSource interface{}) (*node.NodeRunResult, error) {
	providerImpl, err := asrprovider.GetASRProvider(n.data.Provider)
	if err != nil {
		return failResult(newError(ASRConfigMissing, "ASR provider unavailable: "+n.data.Provider, err)), nil
	}

	transReq, sourceMeta, err := n.buildTranscribeRequest(ctx, rawSource)
	if err != nil {
		return failResult(err), nil
	}
	transReq.Options = n.data.Options
	transReq.SourceType = asrprovider.ASRAudioSourceType(n.data.AudioSource.Type)

	execCtx, cancel := context.WithTimeout(ctx, n.data.GetTimeout())
	defer cancel()

	start := time.Now()
	maxRetries := n.data.GetMaxRetries()
	var result *asrprovider.ASRTranscribeResult
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err = providerImpl.Transcribe(execCtx, transReq)
		if err == nil {
			break
		}
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			break
		}
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
		}
	}

	if err != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return failResult(newError(ASRTimeout, "ASR execution timeout", err)), nil
		}
		return failResult(newError(ASRProviderCallFailed, "ASR provider execution failed", err)), nil
	}
	if result == nil {
		return failResult(newError(ASRProviderCallFailed, "ASR provider returned nil result", nil)), nil
	}

	elapsed := time.Since(start).Milliseconds()
	outputs := map[string]interface{}{
		"text":       result.Text,
		"segments":   result.Segments,
		"request_id": result.RequestID,
		"provider":   result.Provider,
		"elapsed_ms": elapsed,
	}

	meta := map[string]interface{}{
		"audio_source_type": n.data.AudioSource.Type,
		"timeout_ms":        n.data.GetTimeout().Milliseconds(),
		"max_retries":       maxRetries,
		"request_id":        result.RequestID,
		"provider":          result.Provider,
	}
	for k, v := range sourceMeta {
		meta[k] = v
	}

	return &node.NodeRunResult{
		Status:   types.NodeExecutionStatusSucceeded,
		Outputs:  outputs,
		Metadata: meta,
	}, nil
}

func (n *ASRNode) runAsync(ctx context.Context, rawSource interface{}) (*node.NodeRunResult, error) {
	asyncProvider, err := asrprovider.GetASRAsyncProvider(n.data.Provider)
	if err != nil {
		return failResult(newError(ASRConfigMissing, "ASR async provider unavailable: "+n.data.Provider, err)), nil
	}

	submitReq, sourceMeta, err := n.buildAsyncSubmitRequest(ctx, rawSource)
	if err != nil {
		return failResult(err), nil
	}
	submitReq.Options = n.data.Options

	if cbURL, cbToken := n.buildCallback(); cbURL != "" {
		submitReq.CallbackURL = cbURL
		submitReq.CallbackToken = cbToken
	}

	maxRetries := n.data.GetMaxRetries()
	start := time.Now()
	var submitResult *asrprovider.ASRAsyncSubmitResult
	for attempt := 0; attempt < maxRetries; attempt++ {
		submitResult, err = asyncProvider.SubmitTask(ctx, submitReq)
		if err == nil {
			break
		}
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
		}
	}
	if err != nil {
		return failResult(newError(ASRProviderCallFailed, "ASR async submit failed", err)), nil
	}
	if submitResult == nil || strings.TrimSpace(submitResult.ProviderTaskRef) == "" {
		return failResult(newError(ASRProviderCallFailed, "ASR async submit returned empty provider_task_ref", nil)), nil
	}

	asyncTaskMeta := map[string]interface{}{
		"task_type":         "asr",
		"provider":          n.data.Provider,
		"provider_task_ref": submitResult.ProviderTaskRef,
		"status":            normalizeAsyncStatus(submitResult.Status),
		"callback_mode":     callbackModeFromURL(submitReq.CallbackURL),
		"callback_token":    submitReq.CallbackToken,
		"callback_url":      submitReq.CallbackURL,
		"submit_payload":    submitResult.Raw,
		"request_id":        submitResult.RequestID,
		"audio_source_type": n.data.AudioSource.Type,
		"poll_interval_ms":  n.data.GetPollInterval().Milliseconds(),
		"wait_timeout_ms":   n.data.GetWaitTimeout().Milliseconds(),
		"submitted_at":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range sourceMeta {
		asyncTaskMeta[k] = v
	}

	if n.data.AsyncMode == "submit_only" {
		return &node.NodeRunResult{
			Status: types.NodeExecutionStatusSucceeded,
			Outputs: map[string]interface{}{
				"provider":          n.data.Provider,
				"provider_task_ref": submitResult.ProviderTaskRef,
				"request_id":        submitResult.RequestID,
				"status":            normalizeAsyncStatus(submitResult.Status),
				"async_mode":        n.data.AsyncMode,
				"elapsed_ms":        time.Since(start).Milliseconds(),
			},
			Metadata: map[string]interface{}{
				"provider":            n.data.Provider,
				"audio_source_type":   n.data.AudioSource.Type,
				"external_async_task": asyncTaskMeta,
			},
		}, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, n.data.GetWaitTimeout())
	defer cancel()

	pollInterval := n.data.GetPollInterval()
	pollCount := 0
	for {
		if err := waitCtx.Err(); err != nil {
			return failResult(newError(ASRTimeout, "ASR async submit_and_wait timeout", err)), nil
		}

		pollCount++
		queryResult, qErr := asyncProvider.QueryTask(waitCtx, submitResult.ProviderTaskRef)
		if qErr != nil {
			return failResult(newError(ASRProviderCallFailed, "ASR async query failed", qErr)), nil
		}
		if queryResult == nil {
			return failResult(newError(ASRProviderCallFailed, "ASR async query returned nil", nil)), nil
		}

		status := normalizeAsyncStatus(queryResult.Status)
		asyncTaskMeta["status"] = status
		asyncTaskMeta["poll_count"] = pollCount
		asyncTaskMeta["query_payload"] = queryResult.Raw
		asyncTaskMeta["request_id"] = nonEmpty(queryResult.RequestID, submitResult.RequestID)

		switch status {
		case "succeeded":
			asyncTaskMeta["completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			asyncTaskMeta["normalized_result"] = map[string]interface{}{
				"text":     queryResult.Text,
				"segments": queryResult.Segments,
			}
			return &node.NodeRunResult{
				Status: types.NodeExecutionStatusSucceeded,
				Outputs: map[string]interface{}{
					"text":              queryResult.Text,
					"segments":          queryResult.Segments,
					"provider":          n.data.Provider,
					"provider_task_ref": submitResult.ProviderTaskRef,
					"request_id":        nonEmpty(queryResult.RequestID, submitResult.RequestID),
					"status":            status,
					"async_mode":        n.data.AsyncMode,
					"elapsed_ms":        time.Since(start).Milliseconds(),
				},
				Metadata: map[string]interface{}{
					"provider":            n.data.Provider,
					"audio_source_type":   n.data.AudioSource.Type,
					"external_async_task": asyncTaskMeta,
				},
			}, nil
		case "failed", "canceled", "expired", "timeout":
			asyncTaskMeta["completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			asyncTaskMeta["error_code"] = queryResult.ErrorCode
			asyncTaskMeta["error_message"] = queryResult.ErrorMessage
			msg := strings.TrimSpace(queryResult.ErrorMessage)
			if msg == "" {
				msg = "ASR async task failed"
			}
			return failResult(newError(ASRProviderCallFailed, msg, nil)), nil
		default:
			select {
			case <-waitCtx.Done():
				return failResult(newError(ASRTimeout, "ASR async submit_and_wait timeout", waitCtx.Err())), nil
			case <-time.After(pollInterval):
			}
		}
	}
}

func (n *ASRNode) buildCallback() (string, string) {
	cfg := asrprovider.GetASRAsyncRuntimeConfig()
	base := strings.TrimRight(strings.TrimSpace(cfg.CallbackBaseURL), "/")
	if base == "" {
		return "", ""
	}
	token := uuid.NewString()
	return base + "/api/v1/callbacks/async-tasks/" + n.data.Provider + "?token=" + token, token
}

func callbackModeFromURL(cbURL string) string {
	if strings.TrimSpace(cbURL) == "" {
		return "none"
	}
	return "provider_callback"
}

func normalizeAsyncStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "waiting", "queued", "submitted":
		return "submitted"
	case "doing", "processing", "running", "in_progress":
		return "running"
	case "success", "succeeded", "completed":
		return "succeeded"
	case "failed", "failure", "error":
		return "failed"
	case "canceled", "cancelled":
		return "canceled"
	case "expired":
		return "expired"
	case "timeout", "timed_out":
		return "timeout"
	default:
		return "running"
	}
}

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (n *ASRNode) buildTranscribeRequest(ctx context.Context, rawSource interface{}) (*asrprovider.ASRTranscribeRequest, map[string]interface{}, error) {
	runtimeCfg := asrprovider.GetASRRuntimeConfig()
	maxAudioBytes := int64(runtimeCfg.MaxAudioMB) << 20
	meta := map[string]interface{}{}

	switch n.data.AudioSource.Type {
	case "audio_url":
		audioURL, ok := rawSource.(string)
		if !ok || strings.TrimSpace(audioURL) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_url source must be non-empty string", nil)
		}
		bytes, filename, contentType, err := fetchAudioByURLWithRetry(ctx, audioURL, runtimeCfg.URLFetchTimeoutMS, maxAudioBytes, n.data.GetMaxRetries())
		if err != nil {
			return nil, nil, err
		}
		meta["source_url"] = sanitizeURLForLog(audioURL)
		meta["source_size_bytes"] = len(bytes)
		return &asrprovider.ASRTranscribeRequest{
			AudioBytes:  bytes,
			Filename:    filename,
			ContentType: contentType,
			AudioURL:    audioURL,
		}, meta, nil

	case "audio_base64":
		s, ok := rawSource.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_base64 source must be non-empty string", nil)
		}
		s = strings.TrimSpace(s)
		if runtimeCfg.MaxBase64Chars > 0 && len(s) > runtimeCfg.MaxBase64Chars {
			return nil, nil, newError(ASRInputTooLarge, "audio_base64 length exceeds limit", nil)
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, nil, newError(ASRBase64DecodeFailed, "decode audio_base64 failed", err)
		}
		if maxAudioBytes > 0 && int64(len(decoded)) > maxAudioBytes {
			return nil, nil, newError(ASRInputTooLarge, "decoded audio size exceeds limit", nil)
		}
		meta["source_size_bytes"] = len(decoded)
		return &asrprovider.ASRTranscribeRequest{AudioBytes: decoded}, meta, nil

	case "audio_file":
		sourceObj, ok := rawSource.(map[string]interface{})
		if !ok {
			return nil, nil, newError(ASRInvalidInput, "audio_file source must be object", nil)
		}
		tempPath, _ := sourceObj["temp_path"].(string)
		if strings.TrimSpace(tempPath) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_file.temp_path is required", nil)
		}
		if err := validateFilePath(tempPath, runtimeCfg.TempDir); err != nil {
			return nil, nil, err
		}
		st, err := os.Stat(tempPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, newError(ASRFileNotFound, "audio file not found", err)
			}
			return nil, nil, newError(ASRFilePathInvalid, "stat audio file failed", err)
		}
		if st.IsDir() {
			return nil, nil, newError(ASRFilePathInvalid, "audio_file.temp_path points to directory", nil)
		}
		if maxAudioBytes > 0 && st.Size() > maxAudioBytes {
			return nil, nil, newError(ASRInputTooLarge, "audio_file size exceeds limit", nil)
		}
		bytes, err := os.ReadFile(tempPath)
		if err != nil {
			return nil, nil, newError(ASRFilePathInvalid, "read audio file failed", err)
		}
		filename := readMapString(sourceObj, "filename")
		contentType := readMapString(sourceObj, "content_type")
		meta["source_file"] = tempPath
		meta["source_size_bytes"] = len(bytes)
		return &asrprovider.ASRTranscribeRequest{
			AudioBytes:  bytes,
			Filename:    filename,
			ContentType: contentType,
		}, meta, nil

	default:
		return nil, nil, newError(ASRInvalidInput, "unsupported audio_source.type", nil)
	}
}

func (n *ASRNode) buildAsyncSubmitRequest(ctx context.Context, rawSource interface{}) (*asrprovider.ASRAsyncSubmitRequest, map[string]interface{}, error) {
	runtimeCfg := asrprovider.GetASRRuntimeConfig()
	maxAudioBytes := int64(runtimeCfg.MaxAudioMB) << 20
	meta := map[string]interface{}{}

	switch n.data.AudioSource.Type {
	case "audio_url":
		audioURL, ok := rawSource.(string)
		if !ok || strings.TrimSpace(audioURL) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_url source must be non-empty string", nil)
		}
		meta["source_url"] = sanitizeURLForLog(audioURL)
		return &asrprovider.ASRAsyncSubmitRequest{
			SourceType: asrprovider.ASRAudioSourceTypeURL,
			AudioURL:   strings.TrimSpace(audioURL),
		}, meta, nil

	case "audio_base64":
		s, ok := rawSource.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_base64 source must be non-empty string", nil)
		}
		s = strings.TrimSpace(s)
		if runtimeCfg.MaxBase64Chars > 0 && len(s) > runtimeCfg.MaxBase64Chars {
			return nil, nil, newError(ASRInputTooLarge, "audio_base64 length exceeds limit", nil)
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, nil, newError(ASRBase64DecodeFailed, "decode audio_base64 failed", err)
		}
		if maxAudioBytes > 0 && int64(len(decoded)) > maxAudioBytes {
			return nil, nil, newError(ASRInputTooLarge, "decoded audio size exceeds limit", nil)
		}
		meta["source_size_bytes"] = len(decoded)
		return &asrprovider.ASRAsyncSubmitRequest{
			SourceType:  asrprovider.ASRAudioSourceTypeBase64,
			AudioBase64: s,
		}, meta, nil

	case "audio_file":
		sourceObj, ok := rawSource.(map[string]interface{})
		if !ok {
			return nil, nil, newError(ASRInvalidInput, "audio_file source must be object", nil)
		}
		tempPath, _ := sourceObj["temp_path"].(string)
		if strings.TrimSpace(tempPath) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_file.temp_path is required", nil)
		}
		if err := validateFilePath(tempPath, runtimeCfg.TempDir); err != nil {
			return nil, nil, err
		}
		st, err := os.Stat(tempPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, newError(ASRFileNotFound, "audio file not found", err)
			}
			return nil, nil, newError(ASRFilePathInvalid, "stat audio file failed", err)
		}
		if st.IsDir() {
			return nil, nil, newError(ASRFilePathInvalid, "audio_file.temp_path points to directory", nil)
		}
		if maxAudioBytes > 0 && st.Size() > maxAudioBytes {
			return nil, nil, newError(ASRInputTooLarge, "audio_file size exceeds limit", nil)
		}
		bytes, err := os.ReadFile(tempPath)
		if err != nil {
			return nil, nil, newError(ASRFilePathInvalid, "read audio file failed", err)
		}
		meta["source_file"] = tempPath
		meta["source_size_bytes"] = len(bytes)
		return &asrprovider.ASRAsyncSubmitRequest{
			SourceType:  asrprovider.ASRAudioSourceTypeFile,
			AudioBase64: base64.StdEncoding.EncodeToString(bytes),
		}, meta, nil
	default:
		return nil, nil, newError(ASRInvalidInput, "unsupported audio_source.type", nil)
	}
}

func fetchAudioByURLWithRetry(ctx context.Context, rawURL string, timeoutMS int, maxBytes int64, maxRetries int) ([]byte, string, string, error) {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		data, filename, ct, err := fetchAudioByURL(ctx, rawURL, timeoutMS, maxBytes)
		if err == nil {
			return data, filename, ct, nil
		}
		lastErr = err
		if attempt < maxRetries-1 {
			select {
			case <-ctx.Done():
				return nil, "", "", newError(ASRURLFetchFailed, "fetch audio_url canceled", ctx.Err())
			case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
			}
		}
	}
	return nil, "", "", lastErr
}

func fetchAudioByURL(ctx context.Context, rawURL string, timeoutMS int, maxBytes int64) ([]byte, string, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", "", newError(ASRURLBlocked, "invalid audio_url", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, "", "", newError(ASRURLBlocked, "audio_url must use http or https", nil)
	}
	if err := validatePublicHost(u.Hostname()); err != nil {
		return nil, "", "", newError(ASRURLBlocked, "audio_url host is blocked", err)
	}

	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	client := &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", "", newError(ASRURLFetchFailed, "build audio url request failed", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", newError(ASRURLFetchFailed, "fetch audio_url failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", newError(ASRURLFetchFailed, fmt.Sprintf("audio_url returned status %d", resp.StatusCode), nil)
	}

	if maxBytes <= 0 {
		maxBytes = 50 << 20
	}
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", "", newError(ASRURLFetchFailed, "read audio_url body failed", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", "", newError(ASRInputTooLarge, "audio_url size exceeds limit", nil)
	}

	filename := filepath.Base(u.Path)
	return data, filename, resp.Header.Get("Content-Type"), nil
}

func validatePublicHost(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip) {
			return fmt.Errorf("private/local IP not allowed: %s", ip.String())
		}
	}
	return nil
}

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}

func validateFilePath(path string, baseDir string) error {
	if strings.TrimSpace(path) == "" {
		return newError(ASRFilePathInvalid, "audio_file.temp_path is empty", nil)
	}
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "/tmp/flowweave-asr"
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return newError(ASRFilePathInvalid, "resolve absolute path failed", err)
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return newError(ASRFilePathInvalid, "resolve base dir failed", err)
	}
	if absPath != absBase && !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) {
		return newError(ASRFilePathInvalid, "audio file path is outside ASR_TEMP_DIR", nil)
	}
	return nil
}

func sanitizeURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func readMapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func failResult(err error) *node.NodeRunResult {
	return &node.NodeRunResult{
		Status: types.NodeExecutionStatusFailed,
		Error:  err.Error(),
	}
}
