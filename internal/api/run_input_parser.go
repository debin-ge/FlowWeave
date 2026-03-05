package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RunInputConfig controls parsing and storage behavior for run inputs.
type RunInputConfig struct {
	ASRTempDir    string
	ASRMaxAudioMB int
}

func normalizeRunInputConfig(cfg RunInputConfig) RunInputConfig {
	if strings.TrimSpace(cfg.ASRTempDir) == "" {
		cfg.ASRTempDir = "/tmp/flowweave-asr"
	}
	if cfg.ASRMaxAudioMB <= 0 {
		cfg.ASRMaxAudioMB = 50
	}
	return cfg
}

func parseRunWorkflowRequest(r *http.Request, cfg RunInputConfig) (*runWorkflowRequest, error) {
	cfg = normalizeRunInputConfig(cfg)

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return parseMultipartRunWorkflowRequest(r, cfg)
	}

	var req runWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Inputs = make(map[string]interface{})
	}
	if req.Inputs == nil {
		req.Inputs = make(map[string]interface{})
	}
	return &req, nil
}

func parseMultipartRunWorkflowRequest(r *http.Request, cfg RunInputConfig) (*runWorkflowRequest, error) {
	limitBytes := int64(cfg.ASRMaxAudioMB) << 20
	if err := r.ParseMultipartForm(limitBytes); err != nil {
		return nil, fmt.Errorf("failed to parse multipart form: %w", err)
	}

	req := &runWorkflowRequest{Inputs: make(map[string]interface{})}
	if v := strings.TrimSpace(r.FormValue("conversation_id")); v != "" {
		req.ConversationID = v
	}

	if rawInputs := strings.TrimSpace(r.FormValue("inputs")); rawInputs != "" {
		if err := json.Unmarshal([]byte(rawInputs), &req.Inputs); err != nil {
			return nil, fmt.Errorf("invalid inputs JSON in multipart form: %w", err)
		}
		if req.Inputs == nil {
			req.Inputs = make(map[string]interface{})
		}
	}

	file, header, err := r.FormFile("audio_file")
	if err != nil {
		if !errors.Is(err, http.ErrMissingFile) {
			return nil, fmt.Errorf("read multipart audio_file failed: %w", err)
		}
		return req, nil
	}
	defer file.Close()

	fileInfo, err := persistAudioFile(file, header, cfg.ASRTempDir, limitBytes)
	if err != nil {
		return nil, err
	}
	req.Inputs["audio_file"] = fileInfo
	return req, nil
}

func persistAudioFile(file multipart.File, header *multipart.FileHeader, tempDir string, maxBytes int64) (map[string]interface{}, error) {
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create ASR_TEMP_DIR: %w", err)
	}

	now := time.Now()
	subDir := filepath.Join(tempDir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create upload subdir: %w", err)
	}

	filename := sanitizeFilename(header.Filename)
	if filename == "" {
		filename = "audio.bin"
	}
	target := filepath.Join(subDir, uuid.NewString()+"-"+filename)

	out, err := os.Create(target)
	if err != nil {
		return nil, fmt.Errorf("failed to create audio temp file: %w", err)
	}
	defer out.Close()

	h := sha256.New()
	limitedReader := io.LimitReader(file, maxBytes+1)
	written, err := io.Copy(io.MultiWriter(out, h), limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to write audio temp file: %w", err)
	}
	if written > maxBytes {
		_ = os.Remove(target)
		return nil, fmt.Errorf("audio file exceeds size limit (%dMB)", maxBytes>>20)
	}

	return map[string]interface{}{
		"filename":     filename,
		"content_type": header.Header.Get("Content-Type"),
		"size_bytes":   written,
		"temp_path":    target,
		"sha256":       hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	return name
}
