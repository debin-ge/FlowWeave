package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"flowweave/internal/domain/workflow/port"
	applog "flowweave/internal/platform/log"
)

// AsyncRunManagerConfig controls async workflow execution workers.
type AsyncRunManagerConfig struct {
	Workers      int
	PollInterval time.Duration
	RunTimeout   time.Duration
}

// AsyncRunManager pulls queued runs from DB and executes them in background.
type AsyncRunManager struct {
	repo   port.Repository
	runner *WorkflowRunner
	cfg    AsyncRunManagerConfig
}

func NewAsyncRunManager(repo port.Repository, runner *WorkflowRunner, cfg AsyncRunManagerConfig) *AsyncRunManager {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 5 * time.Minute
	}
	return &AsyncRunManager{
		repo:   repo,
		runner: runner,
		cfg:    cfg,
	}
}

func (m *AsyncRunManager) Start(ctx context.Context) {
	for i := 0; i < m.cfg.Workers; i++ {
		workerID := fmt.Sprintf("async-worker-%d", i+1)
		go m.workerLoop(ctx, workerID)
	}
	applog.Info("[AsyncRun] Manager started", "workers", m.cfg.Workers, "poll_interval_ms", m.cfg.PollInterval.Milliseconds())
}

func (m *AsyncRunManager) workerLoop(ctx context.Context, workerID string) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		run, err := m.repo.ClaimNextQueuedRun(ctx, workerID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			applog.Error("[AsyncRun] Failed to claim queued run", "worker_id", workerID, "error", err)
			if !sleepWithContext(ctx, m.cfg.PollInterval) {
				return
			}
			continue
		}
		if run == nil {
			if !sleepWithContext(ctx, m.cfg.PollInterval) {
				return
			}
			continue
		}

		m.executeRun(ctx, run, workerID)
	}
}

func (m *AsyncRunManager) executeRun(ctx context.Context, run *port.WorkflowRun, workerID string) {
	if run == nil {
		return
	}

	repoCtx := context.Background()
	if run.OrgID != "" || run.TenantID != "" {
		repoCtx = port.WithRepoScope(repoCtx, run.OrgID, run.TenantID)
	}

	wf, err := m.repo.GetWorkflow(repoCtx, run.WorkflowID)
	if err != nil || wf == nil {
		if err == nil {
			err = fmt.Errorf("workflow not found")
		}
		m.finalizeFailedRun(repoCtx, run, err, workerID, port.RunStatusFailed, nil)
		return
	}

	inputs := make(map[string]interface{})
	if len(run.Inputs) > 0 {
		if err := json.Unmarshal(run.Inputs, &inputs); err != nil {
			m.finalizeFailedRun(repoCtx, run, fmt.Errorf("invalid run inputs JSON: %w", err), workerID, port.RunStatusFailed, nil)
			return
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, m.cfg.RunTimeout)
	defer cancel()

	opts := &RunOptions{
		ConversationID: run.ConversationID,
		OrgID:          run.OrgID,
		TenantID:       run.TenantID,
	}
	startTime := time.Now()
	result, execErr := m.runner.RunSync(execCtx, wf.DSL, inputs, opts)
	elapsed := time.Since(startTime).Milliseconds()

	now := time.Now()
	run.WorkerID = workerID
	run.ElapsedMs = elapsed
	run.FinishedAt = &now

	status := port.RunStatusSucceeded
	if execErr != nil {
		status = port.RunStatusFailed
		if errors.Is(execCtx.Err(), context.Canceled) {
			status = port.RunStatusAborted
		}
	}
	run.Status = status
	if execErr != nil {
		run.Error = execErr.Error()
		run.Outputs = nil
	} else if result != nil && result.Outputs != nil {
		outputsJSON, _ := json.Marshal(result.Outputs)
		run.Outputs = outputsJSON
		run.Error = ""
	}

	var nodeExecs []port.NodeExecution
	if result != nil && len(result.NodeExecutions) > 0 {
		nodeExecs = result.NodeExecutions
	}
	if err := m.persistRunAndNodeExecs(repoCtx, run, nodeExecs); err != nil {
		applog.Error("[AsyncRun] Failed to persist run result", "run_id", run.ID, "worker_id", workerID, "error", err)
	}

	if run.ConversationID != "" && len(nodeExecs) > 0 {
		if err := m.saveTraces(repoCtx, run.ConversationID, run.ID, run.OrgID, run.TenantID, nodeExecs); err != nil {
			applog.Error("[AsyncRun] Failed to persist traces", "run_id", run.ID, "worker_id", workerID, "error", err)
		}
	}
}

func (m *AsyncRunManager) finalizeFailedRun(ctx context.Context, run *port.WorkflowRun, execErr error, workerID string, status port.RunStatus, nodeExecs []port.NodeExecution) {
	now := time.Now()
	run.WorkerID = workerID
	run.Status = status
	run.Error = execErr.Error()
	run.ElapsedMs = 0
	run.FinishedAt = &now
	run.Outputs = nil
	if err := m.persistRunAndNodeExecs(ctx, run, nodeExecs); err != nil {
		applog.Error("[AsyncRun] Failed to persist failed run", "run_id", run.ID, "error", err)
	}
}

func (m *AsyncRunManager) persistRunAndNodeExecs(ctx context.Context, run *port.WorkflowRun, nodeExecs []port.NodeExecution) error {
	if run == nil {
		return nil
	}
	if len(nodeExecs) > 0 {
		records := toNodeExecRecords(run.ID, nodeExecs)
		if err := m.repo.BatchCreateNodeExecs(ctx, records); err != nil {
			return err
		}
	}
	externalTasks := BuildExternalAsyncTasksFromNodeExecs(run, nodeExecs)
	for _, task := range externalTasks {
		if err := m.repo.CreateExternalAsyncTask(ctx, task); err != nil {
			applog.Error("[AsyncRun] Failed to save external async task",
				"run_id", run.ID,
				"node_id", task.NodeID,
				"provider", task.Provider,
				"provider_task_ref", task.ProviderTaskRef,
				"error", err,
			)
		}
	}
	return m.repo.UpdateRun(ctx, run)
}

func (m *AsyncRunManager) saveTraces(ctx context.Context, conversationID string, runID, orgID, tenantID string, nodeExecs []port.NodeExecution) error {
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

		trace := &port.LLMCallTrace{
			Timestamp: exec.StartedAt,
			RunID:     runID,
			NodeID:    exec.NodeID,
			ElapsedMs: exec.ElapsedMs,
			Request:   &port.LLMTraceRequest{},
		}
		if p, ok := traceMap["provider"].(string); ok {
			trace.Provider = p
		}
		if model, ok := traceMap["model"].(string); ok {
			trace.Model = model
		}
		if temp, ok := traceMap["temperature"].(float64); ok {
			trace.Request.Temperature = temp
		}
		if maxTok, ok := traceMap["max_tokens"].(int); ok {
			trace.Request.MaxTokens = maxTok
		}
		if topP, ok := traceMap["top_p"].(float64); ok {
			trace.Request.TopP = topP
		}
		if msgsIface, ok := traceMap["messages"].([]interface{}); ok {
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
		if resp, ok := traceMap["response"].(string); ok {
			trace.Response = &port.LLMTraceResponse{Content: resp}
		}
		if elapsed, ok := traceMap["elapsed_ms"].(float64); ok {
			trace.ElapsedMs = int64(elapsed)
		}

		if err := m.repo.AppendTrace(ctx, conversationID, trace); err != nil {
			return err
		}

		record := &port.LLMCallTraceRecord{
			RunID:          runID,
			ConversationID: conversationID,
			OrgID:          orgID,
			TenantID:       tenantID,
			NodeID:         exec.NodeID,
			Provider:       trace.Provider,
			Model:          trace.Model,
			Request:        trace.Request,
			Response:       trace.Response,
			ElapsedMs:      trace.ElapsedMs,
		}
		if err := m.repo.CreateLLMTrace(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

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

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
