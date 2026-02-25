package api

import (
	"context"
	"encoding/json"
	applog "flowweave/internal/platform/log"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"flowweave/internal/app/workflow"
	"flowweave/internal/domain/workflow/engine"
	"flowweave/internal/domain/workflow/event"
	"flowweave/internal/domain/workflow/port"
)

// WorkflowHandler 工作流 API 处理器
type WorkflowHandler struct {
	repo       port.Repository
	runner     *workflow.WorkflowRunner
	runTimeout time.Duration
}

// NewWorkflowHandler 创建处理器
func NewWorkflowHandler(repo port.Repository, runner *workflow.WorkflowRunner, runTimeout time.Duration) *WorkflowHandler {
	if runner == nil {
		runner = workflow.NewWorkflowRunner(engine.DefaultConfig(), nil)
	}
	if runTimeout <= 0 {
		runTimeout = 5 * time.Minute
	}
	return &WorkflowHandler{repo: repo, runner: runner, runTimeout: runTimeout}
}

// RegisterRoutes 注册路由
func (h *WorkflowHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/workflows", func(r chi.Router) {
		r.Post("/", h.CreateWorkflow)
		r.Get("/", h.ListWorkflows)
		r.Get("/{id}", h.GetWorkflow)
		r.Put("/{id}", h.UpdateWorkflow)
		r.Delete("/{id}", h.DeleteWorkflow)
		r.Post("/{id}/run", h.RunWorkflow)
		r.Post("/{id}/run/stream", h.RunWorkflowStream)
	})
	r.Get("/api/v1/runs/{id}", h.GetRun)
	r.Get("/api/v1/runs/{id}/nodes", h.ListNodeExecutions)
	r.Get("/api/v1/traces/{conversation_id}", h.GetTrace)
}

// injectScope 从请求 context 中获取 Scope 并注入到 repository context
// 返回注入了 scope 的 context；如果没有 scope（兼容模式），返回原始 context
func (h *WorkflowHandler) injectScope(ctx context.Context) (context.Context, *Scope) {
	scope, err := ScopeFrom(ctx)
	if err != nil || scope == nil {
		return ctx, nil
	}
	return port.WithRepoScope(ctx, scope.OrgID, scope.TenantID), scope
}

// ensureConversationOwnership 会话归属写校验
func (h *WorkflowHandler) ensureConversationOwnership(ctx context.Context, conversationID string, scope *Scope) error {
	if scope == nil || conversationID == "" {
		return nil
	}
	err := h.repo.EnsureConversationOwnership(ctx, conversationID, scope.OrgID, scope.TenantID)
	if err != nil && strings.Contains(err.Error(), "conversation_id_conflict") {
		return err
	}
	return err
}

// validateConversationOwnership 会话归属读校验
func (h *WorkflowHandler) validateConversationOwnership(ctx context.Context, conversationID string, scope *Scope) error {
	if scope == nil || conversationID == "" {
		return nil
	}
	return h.repo.ValidateConversationOwnership(ctx, conversationID, scope.OrgID, scope.TenantID)
}

// --- Workflow CRUD ---

func (h *WorkflowHandler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, scope := h.injectScope(r.Context())

	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		DSL         json.RawMessage `json:"dsl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.DSL) == 0 {
		writeError(w, http.StatusBadRequest, "dsl is required")
		return
	}

	wf := &port.Workflow{
		Name:        req.Name,
		Description: req.Description,
		DSL:         req.DSL,
	}
	if scope != nil {
		wf.OrgID = scope.OrgID
		wf.TenantID = scope.TenantID
	}
	if err := h.repo.CreateWorkflow(ctx, wf); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	writeJSON(w, http.StatusCreated, wf)
}

func (h *WorkflowHandler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	wf, err := h.repo.GetWorkflow(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	result, err := h.repo.ListWorkflows(ctx, port.ListWorkflowsParams{
		Page:     page,
		PageSize: pageSize,
		Status:   port.WorkflowStatus(status),
		Search:   search,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type updateWorkflowRequest struct {
	Name        *string          `json:"name,omitempty"`
	Description *string          `json:"description,omitempty"`
	DSL         *json.RawMessage `json:"dsl,omitempty"`
}

func (h *WorkflowHandler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	wf, err := h.repo.GetWorkflow(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	var req updateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Name != nil {
		wf.Name = *req.Name
	}
	if req.Description != nil {
		wf.Description = *req.Description
	}
	if req.DSL != nil {
		wf.DSL = *req.DSL
	}

	if err := h.repo.UpdateWorkflow(ctx, wf); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	// 先检查是否存在且属于当前 scope
	wf, err := h.repo.GetWorkflow(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	if err := h.repo.DeleteWorkflow(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workflow")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "deleted": "true"})
}

// --- 同步执行工作流 ---

type runWorkflowRequest struct {
	Inputs         map[string]interface{} `json:"inputs"`
	ConversationID string                 `json:"conversation_id,omitempty"`
}

func (h *WorkflowHandler) RunWorkflow(w http.ResponseWriter, r *http.Request) {
	ctx, scope := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	// 1. 获取工作流
	wf, err := h.repo.GetWorkflow(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	// 2. 解析输入
	var req runWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Inputs = make(map[string]interface{})
	}

	// 3. 会话归属写校验
	if req.ConversationID != "" {
		if err := h.ensureConversationOwnership(ctx, req.ConversationID, scope); err != nil {
			if strings.Contains(err.Error(), "conversation_id_conflict") {
				writeErrorCode(w, http.StatusConflict, "conversation_id_conflict", "Conversation ID is owned by another tenant")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to verify conversation ownership")
			}
			return
		}
	}

	// 4. 创建执行记录
	inputsJSON, _ := json.Marshal(req.Inputs)
	run := &port.WorkflowRun{
		WorkflowID:     wf.ID,
		ConversationID: req.ConversationID,
		Status:         port.RunStatusRunning,
		Inputs:         inputsJSON,
	}
	if scope != nil {
		run.OrgID = scope.OrgID
		run.TenantID = scope.TenantID
	}
	if err := h.repo.CreateRun(ctx, run); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create run")
		return
	}

	// 5. 同步执行
	startTime := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, h.runTimeout)
	defer cancel()

	opts := &workflow.RunOptions{
		ConversationID: req.ConversationID,
	}
	if scope != nil {
		opts.OrgID = scope.OrgID
		opts.TenantID = scope.TenantID
	}
	result, execErr := h.runner.RunSync(execCtx, wf.DSL, req.Inputs, opts)
	elapsed := time.Since(startTime).Milliseconds()

	// 6. 更新执行记录
	now := time.Now()
	run.ElapsedMs = elapsed
	run.FinishedAt = &now

	if execErr != nil {
		run.Status = port.RunStatusFailed
		run.Error = execErr.Error()
	} else {
		run.Status = port.RunStatusSucceeded
		if result != nil && result.Outputs != nil {
			outputsJSON, _ := json.Marshal(result.Outputs)
			run.Outputs = outputsJSON
		}
	}

	// 6.1 异步落库 run 与 node executions（不阻塞响应）
	persistCtx := context.Background()
	if scope != nil {
		persistCtx = port.WithRepoScope(persistCtx, scope.OrgID, scope.TenantID)
	}
	runSnapshot := *run
	var nodeExecsSnapshot []port.NodeExecution
	if result != nil && len(result.NodeExecutions) > 0 {
		nodeExecsSnapshot = append([]port.NodeExecution(nil), result.NodeExecutions...)
	}
	go h.persistRunAndNodeExecs(persistCtx, &runSnapshot, nodeExecsSnapshot)

	// 7. 保存 LLM 调用溯源（异步，不阻塞响应）
	if req.ConversationID != "" && result != nil {
		traceCtx := context.Background()
		if scope != nil {
			traceCtx = port.WithRepoScope(traceCtx, scope.OrgID, scope.TenantID)
		}
		go h.saveTraces(traceCtx, req.ConversationID, run.ID, result.NodeExecutions)
	}

	// 记录节点执行明细日志（不在响应中返回）
	nodeCount := 0
	if result != nil {
		nodeCount = len(result.NodeExecutions)
	}
	applog.Info("[Workflow/Run] Execution completed",
		"run_id", run.ID,
		"workflow_id", wf.ID,
		"status", run.Status,
		"node_count", nodeCount,
		"elapsed_ms", elapsed,
	)

	if execErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow execution failed: %s", execErr.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run_id":     run.ID,
		"status":     run.Status,
		"outputs":    result.Outputs,
		"elapsed_ms": elapsed,
	})
}

// --- 流式执行工作流 (SSE) ---

func (h *WorkflowHandler) RunWorkflowStream(w http.ResponseWriter, r *http.Request) {
	ctx, scope := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	// 1. 获取工作流
	wf, err := h.repo.GetWorkflow(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get workflow")
		return
	}
	if wf == nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}

	// 2. 解析输入
	var req runWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Inputs = make(map[string]interface{})
	}

	// 3. 会话归属写校验
	if req.ConversationID != "" {
		if err := h.ensureConversationOwnership(ctx, req.ConversationID, scope); err != nil {
			if strings.Contains(err.Error(), "conversation_id_conflict") {
				writeErrorCode(w, http.StatusConflict, "conversation_id_conflict", "Conversation ID is owned by another tenant")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to verify conversation ownership")
			}
			return
		}
	}

	// 4. 创建执行记录
	inputsJSON, _ := json.Marshal(req.Inputs)
	run := &port.WorkflowRun{
		WorkflowID:     wf.ID,
		ConversationID: req.ConversationID,
		Status:         port.RunStatusRunning,
		Inputs:         inputsJSON,
	}
	if scope != nil {
		run.OrgID = scope.OrgID
		run.TenantID = scope.TenantID
	}
	if err := h.repo.CreateRun(ctx, run); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create run")
		return
	}

	// 5. 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Run-ID", run.ID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// 6. 流式执行
	startTime := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, h.runTimeout)
	defer cancel()

	streamOpts := &workflow.RunOptions{
		ConversationID: req.ConversationID,
	}
	if scope != nil {
		streamOpts.OrgID = scope.OrgID
		streamOpts.TenantID = scope.TenantID
	}
	eventCh, execErr := h.runner.RunFromDSL(execCtx, wf.DSL, req.Inputs, streamOpts)
	if execErr != nil {
		sseWriteEvent(w, flusher, "error", map[string]string{"error": execErr.Error()})
		return
	}

	var finalStatus port.RunStatus = port.RunStatusSucceeded
	var finalError string
	var finalOutputs map[string]interface{}
	var finalNodeExecs []port.NodeExecution

	for evt := range eventCh {
		sseData := map[string]interface{}{
			"type": evt.Type,
		}

		if evt.NodeID != "" {
			sseData["node_id"] = evt.NodeID
		}
		if evt.Chunk != "" {
			sseData["chunk"] = evt.Chunk
		}
		if evt.Outputs != nil {
			sseData["outputs"] = evt.Outputs
			finalOutputs = evt.Outputs
		}
		if evt.Error != "" {
			sseData["error"] = evt.Error
		}

		if evt.Type == event.EventTypeGraphRunFailed {
			finalStatus = port.RunStatusFailed
			finalError = evt.Error
			finalNodeExecs = evt.NodeExecutions
		}
		if evt.Type == event.EventTypeGraphRunSucceeded {
			finalNodeExecs = evt.NodeExecutions
		}

		sseWriteEvent(w, flusher, "message", sseData)
	}

	// 7. 更新执行记录
	elapsed := time.Since(startTime).Milliseconds()
	now := time.Now()
	run.Status = finalStatus
	run.Error = finalError
	run.ElapsedMs = elapsed
	run.FinishedAt = &now
	if finalOutputs != nil {
		outputsJSON, _ := json.Marshal(finalOutputs)
		run.Outputs = outputsJSON
	}
	// 7.1 异步落库 run 与 node executions（不阻塞 SSE done 事件）
	persistCtx := context.Background()
	if scope != nil {
		persistCtx = port.WithRepoScope(persistCtx, scope.OrgID, scope.TenantID)
	}
	runSnapshot := *run
	nodeExecsSnapshot := append([]port.NodeExecution(nil), finalNodeExecs...)
	go h.persistRunAndNodeExecs(persistCtx, &runSnapshot, nodeExecsSnapshot)

	// 8. 保存 LLM 调用溯源
	if req.ConversationID != "" && len(finalNodeExecs) > 0 {
		traceCtx := context.Background()
		if scope != nil {
			traceCtx = port.WithRepoScope(traceCtx, scope.OrgID, scope.TenantID)
		}
		go h.saveTraces(traceCtx, req.ConversationID, run.ID, finalNodeExecs)
	}

	// 发送完成事件
	sseWriteEvent(w, flusher, "done", map[string]interface{}{
		"run_id":     run.ID,
		"status":     finalStatus,
		"elapsed_ms": elapsed,
	})
}

func (h *WorkflowHandler) persistRunAndNodeExecs(ctx context.Context, run *port.WorkflowRun, nodeExecs []port.NodeExecution) {
	if run == nil {
		return
	}
	if len(nodeExecs) > 0 {
		records := toNodeExecRecords(run.ID, nodeExecs)
		if err := h.repo.BatchCreateNodeExecs(ctx, records); err != nil {
			applog.Error("[Workflow/Persist] Failed to save node executions", "run_id", run.ID, "error", err)
		}
	}
	if err := h.repo.UpdateRun(ctx, run); err != nil {
		applog.Error("[Workflow/Persist] Failed to update run", "run_id", run.ID, "error", err)
	}
}

// saveTraces 从节点执行明细中提取 LLM 调用记录并保存
func (h *WorkflowHandler) saveTraces(ctx context.Context, conversationID string, runID string, nodeExecs []port.NodeExecution) {
	for _, exec := range nodeExecs {
		if exec.Metadata == nil {
			continue
		}
		traceData, ok := exec.Metadata["llm_trace"]
		if !ok {
			continue
		}

		traceMap, ok := traceData.(map[string]interface{})
		if !ok {
			continue
		}

		// 构建 LLMCallTrace（旧接口兼容）
		trace := &port.LLMCallTrace{
			Timestamp: exec.StartedAt,
			RunID:     runID,
			NodeID:    exec.NodeID,
			ElapsedMs: exec.ElapsedMs,
		}

		if p, ok := traceMap["provider"].(string); ok {
			trace.Provider = p
		}
		if m, ok := traceMap["model"].(string); ok {
			trace.Model = m
		}

		// 构建 request
		trace.Request = &port.LLMTraceRequest{}
		if temp, ok := traceMap["temperature"].(float64); ok {
			trace.Request.Temperature = temp
		}
		if maxTok, ok := traceMap["max_tokens"].(int); ok {
			trace.Request.MaxTokens = maxTok
		}
		if topP, ok := traceMap["top_p"].(float64); ok {
			trace.Request.TopP = topP
		}
		if msgs, ok := traceMap["messages"].([]map[string]string); ok {
			for _, m := range msgs {
				trace.Request.Messages = append(trace.Request.Messages, port.LLMTraceMessage{
					Role:    m["role"],
					Content: m["content"],
				})
			}
		} else if msgsIface, ok := traceMap["messages"].([]interface{}); ok {
			for _, mi := range msgsIface {
				if mm, ok := mi.(map[string]interface{}); ok {
					role, _ := mm["role"].(string)
					content, _ := mm["content"].(string)
					trace.Request.Messages = append(trace.Request.Messages, port.LLMTraceMessage{
						Role:    role,
						Content: content,
					})
				}
			}
		}

		// 构建 response
		if resp, ok := traceMap["response"].(string); ok {
			trace.Response = &port.LLMTraceResponse{
				Content: resp,
			}
		}

		if elapsed, ok := traceMap["elapsed_ms"].(int64); ok {
			trace.ElapsedMs = elapsed
		} else if elapsed, ok := traceMap["elapsed_ms"].(float64); ok {
			trace.ElapsedMs = int64(elapsed)
		}

		// 写入旧 conversation_traces 表（兼容）
		if err := h.repo.AppendTrace(ctx, conversationID, trace); err != nil {
			applog.Error("[Handler/Trace] Failed to save trace",
				"conversation_id", conversationID,
				"node_id", exec.NodeID,
				"error", err,
			)
		}

		// 写入新 llm_call_traces 独立表
		record := &port.LLMCallTraceRecord{
			RunID:          runID,
			ConversationID: conversationID,
			NodeID:         exec.NodeID,
			Provider:       trace.Provider,
			Model:          trace.Model,
			Request:        trace.Request,
			Response:       trace.Response,
			ElapsedMs:      trace.ElapsedMs,
		}
		if err := h.repo.CreateLLMTrace(ctx, record); err != nil {
			applog.Error("[Handler/Trace] Failed to save llm_call_trace",
				"conversation_id", conversationID,
				"node_id", exec.NodeID,
				"error", err,
			)
		} else {
			applog.Info("[Handler/Trace] ✅ Trace saved",
				"conversation_id", conversationID,
				"node_id", exec.NodeID,
				"provider", trace.Provider,
				"model", trace.Model,
			)
		}
	}
}

// toNodeExecRecords 将引擎产出的 NodeExecution 转换为独立表记录
func toNodeExecRecords(runID string, execs []port.NodeExecution) []*port.NodeExecutionRecord {
	records := make([]*port.NodeExecutionRecord, 0, len(execs))
	for _, exec := range execs {
		records = append(records, &port.NodeExecutionRecord{
			RunID:     runID,
			NodeID:    exec.NodeID,
			NodeType:  exec.NodeType,
			Title:     exec.Title,
			Status:    exec.Status,
			Outputs:   exec.Outputs,
			Error:     exec.Error,
			Metadata:  exec.Metadata,
			ElapsedMs: exec.ElapsedMs,
			StartedAt: exec.StartedAt,
		})
	}
	return records
}

// --- 查询执行记录 ---

func (h *WorkflowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())
	id := chi.URLParam(r, "id")

	run, err := h.repo.GetRun(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	// 构建响应 DTO
	writeJSON(w, http.StatusOK, run)
}

// ListNodeExecutions 查询某次执行的节点执行记录
func (h *WorkflowHandler) ListNodeExecutions(w http.ResponseWriter, r *http.Request) {
	ctx, _ := h.injectScope(r.Context())
	runID := chi.URLParam(r, "id")

	records, err := h.repo.ListNodeExecsByRunID(ctx, runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list node executions")
		return
	}
	writeJSON(w, http.StatusOK, records)
}

// --- 查询调用溯源 ---

func (h *WorkflowHandler) GetTrace(w http.ResponseWriter, r *http.Request) {
	ctx, scope := h.injectScope(r.Context())
	convID := chi.URLParam(r, "conversation_id")

	// 读请求：校验会话归属
	if err := h.validateConversationOwnership(ctx, convID, scope); err != nil {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	}

	trace, err := h.repo.GetTrace(ctx, convID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get trace")
		return
	}
	if trace == nil {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	}
	writeJSON(w, http.StatusOK, trace)
}

// --- SSE 辅助 ---

func sseWriteEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
	flusher.Flush()
}
