package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	asrprovider "flowweave/internal/adapter/provider/asr"
	"flowweave/internal/domain/workflow/port"
)

type mockWorkflowRepo struct {
	port.Repository
	wf *port.Workflow
}

func (m *mockWorkflowRepo) GetWorkflow(ctx context.Context, id string) (*port.Workflow, error) {
	return m.wf, nil
}

type mockAsyncOnlyASRProvider struct {
	name string
}

func (m *mockAsyncOnlyASRProvider) Name() string { return m.name }
func (m *mockAsyncOnlyASRProvider) SubmitTask(ctx context.Context, req *asrprovider.ASRAsyncSubmitRequest) (*asrprovider.ASRAsyncSubmitResult, error) {
	return &asrprovider.ASRAsyncSubmitResult{ProviderTaskRef: "test-task", Status: "submitted"}, nil
}
func (m *mockAsyncOnlyASRProvider) QueryTask(ctx context.Context, providerTaskRef string) (*asrprovider.ASRAsyncQueryResult, error) {
	return &asrprovider.ASRAsyncQueryResult{ProviderTaskRef: providerTaskRef, Status: "running"}, nil
}

func TestRunRejectsASRAsyncMode(t *testing.T) {
	dsl := json.RawMessage(`{
		"nodes": [
			{"id":"start_1","data":{"type":"start","title":"Start","variables":[{"variable":"audio_url","type":"string","required":true}]}},
			{"id":"asr_1","data":{"type":"asr","title":"ASR","provider":"azure_batch","async_mode":"submit_only","audio_source":{"type":"audio_url","value_selector":["start_1","audio_url"]}}},
			{"id":"end_1","data":{"type":"end","title":"End","outputs":[]}}
		],
		"edges":[{"source":"start_1","target":"asr_1"},{"source":"asr_1","target":"end_1"}]
	}`)
	repo := &mockWorkflowRepo{wf: &port.Workflow{ID: "wf_1", DSL: dsl}}
	h := NewWorkflowHandler(repo, nil, 0, RunInputConfig{})

	chiRouter := hRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf_1/run", strings.NewReader(`{"inputs":{"audio_url":"https://example.com/a.wav"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	chiRouter.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status=400, got=%d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "workflow_sync_unsupported") {
		t.Fatalf("expected workflow_sync_unsupported, got body=%s", rr.Body.String())
	}
}

func TestRunStreamRejectsASRAsyncOnlyProvider(t *testing.T) {
	const providerName = "test_async_only_provider_for_stream_guard"
	asrprovider.RegisterASRAsyncProvider(&mockAsyncOnlyASRProvider{name: providerName})

	dsl := json.RawMessage(`{
		"nodes": [
			{"id":"start_1","data":{"type":"start","title":"Start","variables":[{"variable":"audio_url","type":"string","required":true}]}},
			{"id":"asr_1","data":{"type":"asr","title":"ASR","provider":"` + providerName + `","audio_source":{"type":"audio_url","value_selector":["start_1","audio_url"]}}},
			{"id":"end_1","data":{"type":"end","title":"End","outputs":[]}}
		],
		"edges":[{"source":"start_1","target":"asr_1"},{"source":"asr_1","target":"end_1"}]
	}`)
	repo := &mockWorkflowRepo{wf: &port.Workflow{ID: "wf_2", DSL: dsl}}
	h := NewWorkflowHandler(repo, nil, 0, RunInputConfig{})
	chiRouter := hRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf_2/run/stream", strings.NewReader(`{"inputs":{"audio_url":"https://example.com/b.wav"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	chiRouter.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status=400, got=%d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "workflow_sync_unsupported") {
		t.Fatalf("expected workflow_sync_unsupported, got body=%s", rr.Body.String())
	}
}

func hRouter(h *WorkflowHandler) http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}
