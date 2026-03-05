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

	"flowweave/internal/domain/workflow/event"
	types "flowweave/internal/domain/workflow/model"
	"flowweave/internal/domain/workflow/node"
	"flowweave/internal/provider"
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

		providerImpl, err := provider.GetASRProvider(n.data.Provider)
		if err != nil {
			return failResult(newError(ASRConfigMissing, "ASR provider unavailable: "+n.data.Provider, err)), nil
		}

		transReq, sourceMeta, err := n.buildTranscribeRequest(ctx, rawSource)
		if err != nil {
			return failResult(err), nil
		}
		transReq.Options = n.data.Options
		transReq.SourceType = provider.ASRAudioSourceType(n.data.AudioSource.Type)

		execCtx, cancel := context.WithTimeout(ctx, n.data.GetTimeout())
		defer cancel()

		start := time.Now()
		maxRetries := n.data.GetMaxRetries()
		var result *provider.ASRTranscribeResult
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
	})
}

func (n *ASRNode) buildTranscribeRequest(ctx context.Context, rawSource interface{}) (*provider.ASRTranscribeRequest, map[string]interface{}, error) {
	runtimeCfg := provider.GetASRRuntimeConfig()
	maxAudioBytes := int64(runtimeCfg.MaxAudioMB) << 20
	meta := map[string]interface{}{}

	switch n.data.AudioSource.Type {
	case "audio_url":
		audioURL, ok := rawSource.(string)
		if !ok || strings.TrimSpace(audioURL) == "" {
			return nil, nil, newError(ASRInvalidInput, "audio_url source must be non-empty string", nil)
		}
		bytes, filename, contentType, err := fetchAudioByURL(ctx, audioURL, runtimeCfg.URLFetchTimeoutMS, maxAudioBytes)
		if err != nil {
			return nil, nil, err
		}
		meta["source_url"] = sanitizeURLForLog(audioURL)
		meta["source_size_bytes"] = len(bytes)
		return &provider.ASRTranscribeRequest{
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
		return &provider.ASRTranscribeRequest{AudioBytes: decoded}, meta, nil

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
		return &provider.ASRTranscribeRequest{
			AudioBytes:  bytes,
			Filename:    filename,
			ContentType: contentType,
		}, meta, nil

	default:
		return nil, nil, newError(ASRInvalidInput, "unsupported audio_source.type", nil)
	}
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
