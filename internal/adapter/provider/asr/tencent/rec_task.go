package tencent

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	tcasr "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/asr/v20190614"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"

	asrprovider "flowweave/internal/adapter/provider/asr"
)

const AsyncProviderName = "tencent_rec_task"

// RecTaskConfig configures Tencent standard async transcription.
type RecTaskConfig struct {
	SecretID          string
	SecretKey         string
	Region            string
	DefaultEngineType string
}

// RecTaskProvider wraps Tencent ASR CreateRecTask/DescribeTaskStatus APIs.
type RecTaskProvider struct {
	client            *tcasr.Client
	defaultEngineType string
}

func NewRecTaskProvider(cfg RecTaskConfig) (*RecTaskProvider, error) {
	if strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("missing Tencent rec-task credentials")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "ap-shanghai"
	}
	engine := strings.TrimSpace(cfg.DefaultEngineType)
	if engine == "" {
		engine = "16k_zh"
	}

	cred := common.NewCredential(cfg.SecretID, cfg.SecretKey)
	cpf := profile.NewClientProfile()
	client, err := tcasr.NewClient(cred, region, cpf)
	if err != nil {
		return nil, err
	}
	return &RecTaskProvider{client: client, defaultEngineType: engine}, nil
}

func (p *RecTaskProvider) Name() string {
	return AsyncProviderName
}

func (p *RecTaskProvider) SubmitTask(ctx context.Context, req *asrprovider.ASRAsyncSubmitRequest) (*asrprovider.ASRAsyncSubmitResult, error) {
	if req == nil {
		return nil, fmt.Errorf("submit request is nil")
	}

	tReq := tcasr.NewCreateRecTaskRequest()
	engine := p.optionString(req.Options, "engine_model_type", p.defaultEngineType)
	tReq.EngineModelType = &engine

	channelNum := uint64(p.optionInt(req.Options, "channel_num", 1))
	resTextFormat := uint64(p.optionInt(req.Options, "res_text_format", 0))
	tReq.ChannelNum = &channelNum
	tReq.ResTextFormat = &resTextFormat

	if strings.TrimSpace(req.CallbackURL) != "" {
		cb := strings.TrimSpace(req.CallbackURL)
		tReq.CallbackUrl = &cb
	}

	if err := p.fillSource(tReq, req); err != nil {
		return nil, err
	}

	filterDirty := int64(p.optionInt(req.Options, "filter_dirty", 0))
	filterModal := int64(p.optionInt(req.Options, "filter_modal", 0))
	filterPunc := int64(p.optionInt(req.Options, "filter_punc", 0))
	convertNumMode := int64(p.optionInt(req.Options, "convert_num_mode", 1))
	speakerDiarization := int64(p.optionInt(req.Options, "speaker_diarization", 0))
	speakerNumber := int64(p.optionInt(req.Options, "speaker_number", 0))

	tReq.FilterDirty = &filterDirty
	tReq.FilterModal = &filterModal
	tReq.FilterPunc = &filterPunc
	tReq.ConvertNumMode = &convertNumMode
	tReq.SpeakerDiarization = &speakerDiarization
	if speakerNumber > 0 {
		tReq.SpeakerNumber = &speakerNumber
	}

	if hotwordID := p.optionString(req.Options, "hotword_id", ""); hotwordID != "" {
		tReq.HotwordId = &hotwordID
	}
	if extra := p.optionString(req.Options, "extra", ""); extra != "" {
		tReq.Extra = &extra
	}

	resp, err := p.client.CreateRecTask(tReq)
	if err != nil {
		return nil, fmt.Errorf("create rec task failed: %w", err)
	}
	if resp == nil || resp.Response == nil || resp.Response.Data == nil || resp.Response.Data.TaskId == nil {
		return nil, fmt.Errorf("create rec task returned empty task id")
	}
	providerTaskRef := strconv.FormatUint(*resp.Response.Data.TaskId, 10)
	requestID := ""
	if resp.Response.RequestId != nil {
		requestID = *resp.Response.RequestId
	}

	return &asrprovider.ASRAsyncSubmitResult{
		ProviderTaskRef: providerTaskRef,
		RequestID:       requestID,
		Status:          "submitted",
		Raw: map[string]interface{}{
			"task_id": providerTaskRef,
		},
	}, nil
}

func (p *RecTaskProvider) QueryTask(ctx context.Context, providerTaskRef string) (*asrprovider.ASRAsyncQueryResult, error) {
	providerTaskRef = strings.TrimSpace(providerTaskRef)
	if providerTaskRef == "" {
		return nil, fmt.Errorf("provider_task_ref is empty")
	}
	taskID, err := strconv.ParseUint(providerTaskRef, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid provider_task_ref: %w", err)
	}

	tReq := tcasr.NewDescribeTaskStatusRequest()
	tReq.TaskId = &taskID
	resp, err := p.client.DescribeTaskStatus(tReq)
	if err != nil {
		return nil, fmt.Errorf("describe task status failed: %w", err)
	}
	if resp == nil || resp.Response == nil || resp.Response.Data == nil {
		return nil, fmt.Errorf("describe task returned empty data")
	}
	data := resp.Response.Data

	status := "running"
	if data.StatusStr != nil {
		switch strings.ToLower(strings.TrimSpace(*data.StatusStr)) {
		case "waiting":
			status = "submitted"
		case "doing":
			status = "running"
		case "success":
			status = "succeeded"
		case "failed":
			status = "failed"
		default:
			status = "running"
		}
	} else if data.Status != nil {
		switch *data.Status {
		case 0:
			status = "submitted"
		case 1:
			status = "running"
		case 2:
			status = "succeeded"
		case 3:
			status = "failed"
		}
	}

	requestID := ""
	if resp.Response.RequestId != nil {
		requestID = *resp.Response.RequestId
	}
	text := ""
	if data.Result != nil {
		text = *data.Result
	}
	errorMessage := ""
	if data.ErrorMsg != nil {
		errorMessage = *data.ErrorMsg
	}

	segments := make([]asrprovider.ASRSegment, 0, len(data.ResultDetail))
	for _, s := range data.ResultDetail {
		if s == nil {
			continue
		}
		seg := asrprovider.ASRSegment{}
		if s.FinalSentence != nil {
			seg.Text = *s.FinalSentence
		}
		if s.StartMs != nil {
			seg.StartMs = int64(*s.StartMs)
		}
		if s.EndMs != nil {
			seg.EndMs = int64(*s.EndMs)
		}
		segments = append(segments, seg)
	}

	res := &asrprovider.ASRAsyncQueryResult{
		ProviderTaskRef: providerTaskRef,
		Status:          status,
		Text:            text,
		Segments:        segments,
		RequestID:       requestID,
		Raw: map[string]interface{}{
			"status":     data.Status,
			"status_str": data.StatusStr,
			"error_msg":  data.ErrorMsg,
		},
	}
	if status == "failed" {
		res.ErrorCode = "TENCENT_REC_TASK_FAILED"
		res.ErrorMessage = errorMessage
	}
	return res, nil
}

func (p *RecTaskProvider) fillSource(tReq *tcasr.CreateRecTaskRequest, req *asrprovider.ASRAsyncSubmitRequest) error {
	sourceType := uint64(0)
	if req.SourceType == asrprovider.ASRAudioSourceTypeURL {
		if strings.TrimSpace(req.AudioURL) == "" {
			return fmt.Errorf("audio_url is required for source type audio_url")
		}
		sourceType = 0
		audioURL := strings.TrimSpace(req.AudioURL)
		tReq.Url = &audioURL
		tReq.SourceType = &sourceType
		return nil
	}

	// For base64/file source, Tencent CreateRecTask needs base64 Data (<=5MB raw audio recommended by provider).
	if strings.TrimSpace(req.AudioBase64) == "" {
		return fmt.Errorf("audio_base64 is required for non-url source")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.AudioBase64))
	if err != nil {
		return fmt.Errorf("invalid audio_base64: %w", err)
	}
	sourceType = 1
	tReq.SourceType = &sourceType
	data := strings.TrimSpace(req.AudioBase64)
	tReq.Data = &data
	dataLen := uint64(len(decoded))
	tReq.DataLen = &dataLen
	return nil
}

func (p *RecTaskProvider) optionString(options map[string]interface{}, key, def string) string {
	if options == nil {
		return def
	}
	if v, ok := options[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (p *RecTaskProvider) optionInt(options map[string]interface{}, key string, def int) int {
	if options == nil {
		return def
	}
	raw, ok := options[key]
	if !ok || raw == nil {
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
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}
