package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	applog "flowweave/internal/platform/log"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"flowweave/internal/domain/workflow/port"
)

type Workflow = port.Workflow
type WorkflowStatus = port.WorkflowStatus
type ListWorkflowsParams = port.ListWorkflowsParams
type ListWorkflowsResult = port.ListWorkflowsResult

type Organization = port.Organization
type Tenant = port.Tenant

type Dataset = port.Dataset
type Document = port.Document

type WorkflowRun = port.WorkflowRun
type RunStatus = port.RunStatus

type NodeExecutionRecord = port.NodeExecutionRecord

type LLMCallTrace = port.LLMCallTrace
type ConversationTrace = port.ConversationTrace
type LLMCallTraceRecord = port.LLMCallTraceRecord
type LLMTraceRequest = port.LLMTraceRequest
type LLMTraceResponse = port.LLMTraceResponse

const (
	WorkflowStatusActive = port.WorkflowStatusActive
	RunStatusRunning     = port.RunStatusRunning
)

type Repository struct {
	db *sql.DB
}

// NewRepository 创建 PostgreSQL 存储
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// EnsureTenantTables 确保多租户相关表存在
func (r *Repository) EnsureTenantTables(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS organizations (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		code       VARCHAR(64) NOT NULL UNIQUE,
		name       VARCHAR(255) NOT NULL,
		status     VARCHAR(32) NOT NULL DEFAULT 'active',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS tenants (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		code       VARCHAR(64) NOT NULL,
		name       VARCHAR(255) NOT NULL,
		status     VARCHAR(32) NOT NULL DEFAULT 'active',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (org_id, code),
		UNIQUE (org_id, id)
	);

	CREATE TABLE IF NOT EXISTS conversations (
		conversation_id VARCHAR(255) PRIMARY KEY,
		org_id          UUID NOT NULL,
		tenant_id       UUID NOT NULL,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (conversation_id, org_id, tenant_id),
		FOREIGN KEY (org_id, tenant_id) REFERENCES tenants(org_id, id)
	);
	CREATE INDEX IF NOT EXISTS idx_conversations_scope ON conversations(org_id, tenant_id);
	`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

// EnsureTenantColumns 确保业务表有 org_id/tenant_id 列
func (r *Repository) EnsureTenantColumns(ctx context.Context) error {
	queries := []string{
		`ALTER TABLE workflows ADD COLUMN IF NOT EXISTS org_id UUID`,
		`ALTER TABLE workflows ADD COLUMN IF NOT EXISTS tenant_id UUID`,
		`ALTER TABLE workflow_runs ADD COLUMN IF NOT EXISTS org_id UUID`,
		`ALTER TABLE workflow_runs ADD COLUMN IF NOT EXISTS tenant_id UUID`,
		`ALTER TABLE conversation_traces ADD COLUMN IF NOT EXISTS org_id UUID`,
		`ALTER TABLE conversation_traces ADD COLUMN IF NOT EXISTS tenant_id UUID`,
		`ALTER TABLE conversation_summaries ADD COLUMN IF NOT EXISTS org_id UUID`,
		`ALTER TABLE conversation_summaries ADD COLUMN IF NOT EXISTS tenant_id UUID`,
		// 索引
		`CREATE INDEX IF NOT EXISTS idx_workflows_scope_updated ON workflows(org_id, tenant_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_scope_started ON workflow_runs(org_id, tenant_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_scope_conv ON workflow_runs(org_id, tenant_id, conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_traces_scope_conv ON conversation_traces(org_id, tenant_id, conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_summaries_scope_conv ON conversation_summaries(org_id, tenant_id, conversation_id)`,
	}
	for _, q := range queries {
		if _, err := r.db.ExecContext(ctx, q); err != nil {
			applog.Warn("[Storage] ALTER TABLE / CREATE INDEX failed (may already exist)", "query", q, "error", err)
		}
	}
	return nil
}

// EnsureTracesTable 确保 conversation_traces 表存在
func (r *Repository) EnsureTracesTable(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS conversation_traces (
		id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		org_id          UUID,
		tenant_id       UUID,
		conversation_id VARCHAR(255) NOT NULL UNIQUE,
		traces          JSONB NOT NULL DEFAULT '[]',
		created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_conv_traces_conv_id ON conversation_traces(conversation_id);
	`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

// EnsureRunColumns 确保 workflow_runs 表有新字段（兼容旧表）
func (r *Repository) EnsureRunColumns(ctx context.Context) error {
	queries := []string{
		`ALTER TABLE workflow_runs ADD COLUMN IF NOT EXISTS conversation_id VARCHAR(255) DEFAULT ''`,
	}
	for _, q := range queries {
		if _, err := r.db.ExecContext(ctx, q); err != nil {
			applog.Warn("[Storage] ALTER TABLE failed (may already exist)", "error", err)
		}
	}
	return nil
}

// EnsureNodeExecTable 确保 node_executions 独立表存在
func (r *Repository) EnsureNodeExecTable(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS node_executions (
		id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		run_id      UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
		node_id     VARCHAR(255) NOT NULL,
		node_type   VARCHAR(64) NOT NULL,
		title       VARCHAR(255) DEFAULT '',
		status      VARCHAR(32) NOT NULL,
		outputs     JSONB,
		error       TEXT DEFAULT '',
		metadata    JSONB,
		elapsed_ms  BIGINT DEFAULT 0,
		started_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
		UNIQUE (run_id, node_id)
	);
	CREATE INDEX IF NOT EXISTS idx_node_exec_run ON node_executions(run_id);
	CREATE INDEX IF NOT EXISTS idx_node_exec_type ON node_executions(node_type);
	CREATE INDEX IF NOT EXISTS idx_node_exec_status ON node_executions(status);
	`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

// EnsureLLMTracesTable 确保 llm_call_traces 独立表存在
func (r *Repository) EnsureLLMTracesTable(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS llm_call_traces (
		id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		run_id          UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
		conversation_id VARCHAR(255) NOT NULL,
		org_id          UUID,
		tenant_id       UUID,
		node_id         VARCHAR(255) NOT NULL,
		provider        VARCHAR(64),
		model           VARCHAR(128),
		request         JSONB,
		response        JSONB,
		elapsed_ms      BIGINT DEFAULT 0,
		error           TEXT DEFAULT '',
		created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_llm_traces_conv ON llm_call_traces(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_llm_traces_run ON llm_call_traces(run_id);
	CREATE INDEX IF NOT EXISTS idx_llm_traces_scope ON llm_call_traces(org_id, tenant_id);
	`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

// --- 会话归属校验 ---

// EnsureConversationOwnership 注册 + 校验会话归属（写请求）
// 如果 conversation_id 已被其他租户占用，返回错误
func (r *Repository) EnsureConversationOwnership(ctx context.Context, conversationID, orgID, tenantID string) error {
	var resultID string
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO conversations(conversation_id, org_id, tenant_id, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (conversation_id) DO UPDATE
		 SET updated_at = NOW()
		 WHERE conversations.org_id = EXCLUDED.org_id
		   AND conversations.tenant_id = EXCLUDED.tenant_id
		 RETURNING conversation_id`,
		conversationID, orgID, tenantID,
	).Scan(&resultID)

	if err == sql.ErrNoRows {
		// ON CONFLICT 匹配但 WHERE 不满足 → 属于其他租户
		return fmt.Errorf("conversation_id_conflict: %s is owned by another tenant", conversationID)
	}
	if err != nil {
		return fmt.Errorf("ensure conversation ownership: %w", err)
	}
	return nil
}

// ValidateConversationOwnership 仅校验会话归属（读请求）
func (r *Repository) ValidateConversationOwnership(ctx context.Context, conversationID, orgID, tenantID string) error {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT conversation_id FROM conversations
		 WHERE conversation_id = $1 AND org_id = $2 AND tenant_id = $3`,
		conversationID, orgID, tenantID,
	).Scan(&id)

	if err == sql.ErrNoRows {
		return fmt.Errorf("conversation not found or not owned by current tenant")
	}
	if err != nil {
		return fmt.Errorf("validate conversation ownership: %w", err)
	}
	return nil
}

// --- Workflow CRUD ---

func (r *Repository) CreateWorkflow(ctx context.Context, w *Workflow) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.Status == "" {
		w.Status = WorkflowStatusActive
	}
	if w.Version == 0 {
		w.Version = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, org_id, tenant_id, name, description, dsl_json, version, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		w.ID, nullIfEmpty(w.OrgID), nullIfEmpty(w.TenantID), w.Name, w.Description, w.DSL, w.Version, w.Status, w.CreatedAt, w.UpdatedAt,
	)
	return err
}

func (r *Repository) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	w := &Workflow{}
	var orgID, tenantID sql.NullString
	query := `SELECT id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''), name, description, dsl_json, version, status, created_at, updated_at
		 FROM workflows WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&w.ID, &orgID, &tenantID, &w.Name, &w.Description, &w.DSL, &w.Version, &w.Status, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	w.OrgID = orgID.String
	w.TenantID = tenantID.String
	return w, err
}

func (r *Repository) UpdateWorkflow(ctx context.Context, w *Workflow) error {
	w.UpdatedAt = time.Now()
	w.Version++
	query := `UPDATE workflows SET name=$1, description=$2, dsl_json=$3, version=$4, status=$5, updated_at=$6
		 WHERE id=$7`
	args := []interface{}{w.Name, w.Description, w.DSL, w.Version, w.Status, w.UpdatedAt, w.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id=$8 AND tenant_id=$9`
		args = append(args, scope.OrgID, scope.TenantID)
	} else {
		query += ` AND ($8::uuid IS NULL OR org_id=$8) AND ($9::uuid IS NULL OR tenant_id=$9)`
		args = append(args, nullIfEmpty(w.OrgID), nullIfEmpty(w.TenantID))
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) DeleteWorkflow(ctx context.Context, id string) error {
	query := `DELETE FROM workflows WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) ListWorkflows(ctx context.Context, params ListWorkflowsParams) (*ListWorkflowsResult, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 || params.PageSize > 100 {
		params.PageSize = 20
	}

	var where []string
	var args []interface{}
	argIdx := 1

	// 从 context 提取 scope（如有）
	if scope := scopeFromContext(ctx); scope != nil {
		where = append(where, fmt.Sprintf("org_id = $%d", argIdx))
		args = append(args, scope.OrgID)
		argIdx++
		where = append(where, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, scope.TenantID)
		argIdx++
	}

	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}
	if params.Search != "" {
		where = append(where, fmt.Sprintf("name ILIKE $%d", argIdx))
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM workflows %s", whereClause)
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	// Query
	offset := (params.Page - 1) * params.PageSize
	query := fmt.Sprintf(
		`SELECT id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''), name, description, dsl_json, version, status, created_at, updated_at
		 FROM workflows %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, params.PageSize, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []*Workflow
	for rows.Next() {
		w := &Workflow{}
		var orgID, tenantID sql.NullString
		if err := rows.Scan(&w.ID, &orgID, &tenantID, &w.Name, &w.Description, &w.DSL, &w.Version, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.OrgID = orgID.String
		w.TenantID = tenantID.String
		workflows = append(workflows, w)
	}

	return &ListWorkflowsResult{
		Workflows: workflows,
		Total:     total,
		Page:      params.Page,
		PageSize:  params.PageSize,
	}, nil
}

// --- WorkflowRun CRUD ---

func (r *Repository) CreateRun(ctx context.Context, run *WorkflowRun) error {
	if run.ID == "" {
		run.ID = uuid.New().String()
	}
	run.StartedAt = time.Now()
	if run.Status == "" {
		run.Status = RunStatusRunning
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_runs (id, workflow_id, org_id, tenant_id, conversation_id, status, inputs, outputs, error, total_tokens, total_steps, elapsed_ms, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		run.ID, run.WorkflowID, nullIfEmpty(run.OrgID), nullIfEmpty(run.TenantID), run.ConversationID, run.Status, run.Inputs, run.Outputs, run.Error,
		run.TotalTokens, run.TotalSteps, run.ElapsedMs, run.StartedAt, run.FinishedAt,
	)
	return err
}

func (r *Repository) GetRun(ctx context.Context, id string) (*WorkflowRun, error) {
	run := &WorkflowRun{}
	var orgID, tenantID sql.NullString
	query := `SELECT id, workflow_id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''), conversation_id, status, inputs, outputs, error, total_tokens, total_steps, elapsed_ms, started_at, finished_at
		 FROM workflow_runs WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&run.ID, &run.WorkflowID, &orgID, &tenantID, &run.ConversationID, &run.Status, &run.Inputs, &run.Outputs, &run.Error,
		&run.TotalTokens, &run.TotalSteps, &run.ElapsedMs, &run.StartedAt, &run.FinishedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	run.OrgID = orgID.String
	run.TenantID = tenantID.String
	return run, err
}

func (r *Repository) UpdateRun(ctx context.Context, run *WorkflowRun) error {
	query := `UPDATE workflow_runs SET status=$1, outputs=$2, error=$3, total_tokens=$4, total_steps=$5, elapsed_ms=$6, finished_at=$7, conversation_id=$8
		 WHERE id=$9`
	args := []interface{}{run.Status, run.Outputs, run.Error, run.TotalTokens, run.TotalSteps, run.ElapsedMs, run.FinishedAt, run.ConversationID, run.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $10 AND tenant_id = $11`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) ListRuns(ctx context.Context, workflowID string, page, pageSize int) ([]*WorkflowRun, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var where []string
	var args []interface{}
	argIdx := 1

	where = append(where, fmt.Sprintf("workflow_id = $%d", argIdx))
	args = append(args, workflowID)
	argIdx++

	// 从 context 提取 scope（如有）
	if scope := scopeFromContext(ctx); scope != nil {
		where = append(where, fmt.Sprintf("org_id = $%d", argIdx))
		args = append(args, scope.OrgID)
		argIdx++
		where = append(where, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, scope.TenantID)
		argIdx++
	}

	whereClause := "WHERE " + strings.Join(where, " AND ")

	query := fmt.Sprintf(
		`SELECT id, workflow_id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''), conversation_id, status, inputs, outputs, error, total_tokens, total_steps, elapsed_ms, started_at, finished_at
		 FROM workflow_runs %s ORDER BY started_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, pageSize, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*WorkflowRun
	for rows.Next() {
		run := &WorkflowRun{}
		var orgID, tenantID sql.NullString
		if err := rows.Scan(&run.ID, &run.WorkflowID, &orgID, &tenantID, &run.ConversationID, &run.Status, &run.Inputs, &run.Outputs, &run.Error,
			&run.TotalTokens, &run.TotalSteps, &run.ElapsedMs, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		run.OrgID = orgID.String
		run.TenantID = tenantID.String
		runs = append(runs, run)
	}
	return runs, nil
}

// --- ConversationTrace ---

func (r *Repository) AppendTrace(ctx context.Context, conversationID string, trace *LLMCallTrace) error {
	traceJSON, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}

	// 获取 scope
	var orgIDVal, tenantIDVal interface{}
	if scope := scopeFromContext(ctx); scope != nil {
		orgIDVal = scope.OrgID
		tenantIDVal = scope.TenantID
	}

	// UPSERT: 如果记录不存在则创建（traces = [trace]），已存在则追加到数组
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO conversation_traces (conversation_id, org_id, tenant_id, traces, updated_at)
		 VALUES ($1, $2, $3, jsonb_build_array($4::jsonb), NOW())
		 ON CONFLICT (conversation_id) DO UPDATE
		 SET traces = conversation_traces.traces || jsonb_build_array($4::jsonb),
		     updated_at = NOW()`,
		conversationID, orgIDVal, tenantIDVal, traceJSON,
	)
	if err != nil {
		return fmt.Errorf("append trace: %w", err)
	}

	applog.Debug("[Storage/Trace] Trace appended",
		"conversation_id", conversationID,
		"provider", trace.Provider,
		"model", trace.Model,
	)
	return nil
}

func (r *Repository) GetTrace(ctx context.Context, conversationID string) (*ConversationTrace, error) {
	ct := &ConversationTrace{}
	var orgID, tenantID sql.NullString

	query := `SELECT id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''), conversation_id, traces, created_at, updated_at
		 FROM conversation_traces WHERE conversation_id = $1`
	args := []interface{}{conversationID}

	// scope 过滤
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&ct.ID, &orgID, &tenantID, &ct.ConversationID, &ct.Traces, &ct.CreatedAt, &ct.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	ct.OrgID = orgID.String
	ct.TenantID = tenantID.String
	return ct, nil
}

// --- NodeExecution 独立表 ---

func (r *Repository) BatchCreateNodeExecs(ctx context.Context, records []*NodeExecutionRecord) error {
	if len(records) == 0 {
		return nil
	}
	// 构建批量 INSERT
	var sb strings.Builder
	sb.WriteString(`INSERT INTO node_executions (id, run_id, node_id, node_type, title, status, outputs, error, metadata, elapsed_ms, started_at) VALUES `)

	args := make([]interface{}, 0, len(records)*11)
	for i, rec := range records {
		if rec.ID == "" {
			rec.ID = uuid.New().String()
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * 11
		sb.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11))

		outputsJSON, _ := json.Marshal(rec.Outputs)
		metadataJSON, _ := json.Marshal(rec.Metadata)
		args = append(args, rec.ID, rec.RunID, rec.NodeID, rec.NodeType, rec.Title, rec.Status,
			outputsJSON, rec.Error, metadataJSON, rec.ElapsedMs, rec.StartedAt)
	}
	sb.WriteString(" ON CONFLICT (run_id, node_id) DO NOTHING")

	_, err := r.db.ExecContext(ctx, sb.String(), args...)
	return err
}

func (r *Repository) ListNodeExecsByRunID(ctx context.Context, runID string) ([]*NodeExecutionRecord, error) {
	query := `SELECT ne.id, ne.run_id, ne.node_id, ne.node_type, ne.title, ne.status, ne.outputs, ne.error, ne.metadata, ne.elapsed_ms, ne.started_at
		 FROM node_executions ne
		 JOIN workflow_runs wr ON wr.id = ne.run_id
		 WHERE ne.run_id = $1`
	args := []interface{}{runID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND wr.org_id = $2 AND wr.tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	query += ` ORDER BY ne.started_at`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*NodeExecutionRecord
	for rows.Next() {
		rec := &NodeExecutionRecord{}
		var outputsJSON, metadataJSON json.RawMessage
		if err := rows.Scan(&rec.ID, &rec.RunID, &rec.NodeID, &rec.NodeType, &rec.Title, &rec.Status,
			&outputsJSON, &rec.Error, &metadataJSON, &rec.ElapsedMs, &rec.StartedAt); err != nil {
			return nil, err
		}
		if len(outputsJSON) > 0 {
			_ = json.Unmarshal(outputsJSON, &rec.Outputs)
		}
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &rec.Metadata)
		}
		records = append(records, rec)
	}
	return records, nil
}

// --- LLMCallTrace 独立表 ---

func (r *Repository) CreateLLMTrace(ctx context.Context, record *LLMCallTraceRecord) error {
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}

	reqJSON, _ := json.Marshal(record.Request)
	respJSON, _ := json.Marshal(record.Response)

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO llm_call_traces (id, run_id, conversation_id, org_id, tenant_id, node_id, provider, model, request, response, elapsed_ms, error, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		record.ID, nullIfEmpty(record.RunID), record.ConversationID,
		nullIfEmpty(record.OrgID), nullIfEmpty(record.TenantID),
		record.NodeID, record.Provider, record.Model,
		reqJSON, respJSON, record.ElapsedMs, record.Error, record.CreatedAt,
	)
	return err
}

func (r *Repository) ListLLMTraces(ctx context.Context, conversationID string) ([]*LLMCallTraceRecord, error) {
	query := `SELECT id, COALESCE(run_id::text,''), conversation_id, COALESCE(org_id::text,''), COALESCE(tenant_id::text,''),
	           node_id, provider, model, request, response, elapsed_ms, error, created_at
	           FROM llm_call_traces WHERE conversation_id = $1`
	args := []interface{}{conversationID}

	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	query += ` ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*LLMCallTraceRecord
	for rows.Next() {
		rec := &LLMCallTraceRecord{}
		var reqJSON, respJSON json.RawMessage
		if err := rows.Scan(&rec.ID, &rec.RunID, &rec.ConversationID, &rec.OrgID, &rec.TenantID,
			&rec.NodeID, &rec.Provider, &rec.Model, &reqJSON, &respJSON, &rec.ElapsedMs, &rec.Error, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(reqJSON) > 0 {
			rec.Request = &LLMTraceRequest{}
			_ = json.Unmarshal(reqJSON, rec.Request)
		}
		if len(respJSON) > 0 {
			rec.Response = &LLMTraceResponse{}
			_ = json.Unmarshal(respJSON, rec.Response)
		}
		records = append(records, rec)
	}
	return records, nil
}

// --- Organization CRUD ---

func (r *Repository) CreateOrganization(ctx context.Context, org *Organization) error {
	if org.ID == "" {
		org.ID = uuid.New().String()
	}
	if org.Status == "" {
		org.Status = "active"
	}
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO organizations (id, code, name, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		org.ID, org.Code, org.Name, org.Status, now, now)
	return err
}

func (r *Repository) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	org := &Organization{}
	query := `SELECT id, code, name, status, created_at, updated_at FROM organizations WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND id = $2`
		args = append(args, scope.OrgID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&org.ID, &org.Code, &org.Name, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return org, err
}

func (r *Repository) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	query := `SELECT id, code, name, status, created_at, updated_at FROM organizations`
	args := []interface{}{}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` WHERE id = $1`
		args = append(args, scope.OrgID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*Organization
	for rows.Next() {
		org := &Organization{}
		if err := rows.Scan(&org.ID, &org.Code, &org.Name, &org.Status, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, org)
	}
	return orgs, nil
}

func (r *Repository) UpdateOrganization(ctx context.Context, org *Organization) error {
	query := `UPDATE organizations SET code=$1, name=$2, status=$3, updated_at=NOW() WHERE id=$4`
	args := []interface{}{org.Code, org.Name, org.Status, org.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND id=$5`
		args = append(args, scope.OrgID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) DeleteOrganization(ctx context.Context, id string) error {
	query := `DELETE FROM organizations WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND id = $2`
		args = append(args, scope.OrgID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// --- Tenant CRUD ---

func (r *Repository) CreateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant.ID == "" {
		tenant.ID = uuid.New().String()
	}
	if tenant.Status == "" {
		tenant.Status = "active"
	}
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tenants (id, org_id, code, name, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenant.ID, tenant.OrgID, tenant.Code, tenant.Name, tenant.Status, now, now)
	return err
}

func (r *Repository) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	t := &Tenant{}
	query := `SELECT id, org_id, code, name, status, created_at, updated_at FROM tenants WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&t.ID, &t.OrgID, &t.Code, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (r *Repository) ListTenants(ctx context.Context, orgID string) ([]*Tenant, error) {
	query := `SELECT id, org_id, code, name, status, created_at, updated_at FROM tenants WHERE 1=1`
	var args []interface{}
	argIdx := 1
	if scope := scopeFromContext(ctx); scope != nil {
		query += fmt.Sprintf(" AND org_id = $%d AND id = $%d", argIdx, argIdx+1)
		args = append(args, scope.OrgID, scope.TenantID)
		argIdx += 2
	} else if orgID != "" {
		query += fmt.Sprintf(" AND org_id = $%d", argIdx)
		args = append(args, orgID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		t := &Tenant{}
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Code, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, nil
}

func (r *Repository) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	query := `UPDATE tenants SET code=$1, name=$2, status=$3, updated_at=NOW() WHERE id=$4`
	args := []interface{}{tenant.Code, tenant.Name, tenant.Status, tenant.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id=$5 AND id=$6`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) DeleteTenant(ctx context.Context, id string) error {
	query := `DELETE FROM tenants WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// --- Dataset CRUD (RAG) ---

func (r *Repository) EnsureRAGTables(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS datasets (
		id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		org_id      UUID NOT NULL,
		tenant_id   UUID NOT NULL,
		name        VARCHAR(255) NOT NULL,
		description TEXT DEFAULT '',
		status      VARCHAR(32) NOT NULL DEFAULT 'active',
		doc_count   INTEGER DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		FOREIGN KEY (org_id, tenant_id) REFERENCES tenants(org_id, id)
	);
	CREATE INDEX IF NOT EXISTS idx_datasets_scope ON datasets(org_id, tenant_id);

	CREATE TABLE IF NOT EXISTS documents (
		id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		dataset_id  UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
		org_id      UUID NOT NULL,
		tenant_id   UUID NOT NULL,
		name        VARCHAR(255) NOT NULL,
		source      VARCHAR(512) DEFAULT '',
		chunk_count INTEGER DEFAULT 0,
		status      VARCHAR(32) DEFAULT 'processing',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_documents_dataset ON documents(dataset_id);
	`
	_, err := r.db.ExecContext(ctx, ddl)
	return err
}

func (r *Repository) CreateDataset(ctx context.Context, ds *Dataset) error {
	if ds.ID == "" {
		ds.ID = uuid.New().String()
	}
	if ds.Status == "" {
		ds.Status = "active"
	}
	now := time.Now()
	ds.CreatedAt = now
	ds.UpdatedAt = now
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO datasets (id, org_id, tenant_id, name, description, status, doc_count, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		ds.ID, ds.OrgID, ds.TenantID, ds.Name, ds.Description, ds.Status, ds.DocCount, ds.CreatedAt, ds.UpdatedAt)
	return err
}

func (r *Repository) GetDataset(ctx context.Context, id string) (*Dataset, error) {
	ds := &Dataset{}
	query := `SELECT id, org_id, tenant_id, name, description, status, doc_count, created_at, updated_at
		 FROM datasets WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&ds.ID, &ds.OrgID, &ds.TenantID, &ds.Name, &ds.Description, &ds.Status, &ds.DocCount, &ds.CreatedAt, &ds.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ds, err
}

func (r *Repository) ListDatasets(ctx context.Context, orgID, tenantID string) ([]*Dataset, error) {
	query := `SELECT id, org_id, tenant_id, name, description, status, doc_count, created_at, updated_at FROM datasets WHERE 1=1`
	var args []interface{}
	argIdx := 1
	if scope := scopeFromContext(ctx); scope != nil {
		query += fmt.Sprintf(" AND org_id = $%d AND tenant_id = $%d", argIdx, argIdx+1)
		args = append(args, scope.OrgID, scope.TenantID)
		argIdx += 2
	} else if orgID != "" {
		query += fmt.Sprintf(" AND org_id = $%d", argIdx)
		args = append(args, orgID)
		argIdx++
	}
	if scope := scopeFromContext(ctx); scope == nil && tenantID != "" {
		query += fmt.Sprintf(" AND tenant_id = $%d", argIdx)
		args = append(args, tenantID)
		argIdx++
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var datasets []*Dataset
	for rows.Next() {
		ds := &Dataset{}
		if err := rows.Scan(&ds.ID, &ds.OrgID, &ds.TenantID, &ds.Name, &ds.Description, &ds.Status, &ds.DocCount, &ds.CreatedAt, &ds.UpdatedAt); err != nil {
			return nil, err
		}
		datasets = append(datasets, ds)
	}
	return datasets, nil
}

func (r *Repository) UpdateDataset(ctx context.Context, ds *Dataset) error {
	ds.UpdatedAt = time.Now()
	query := `UPDATE datasets SET name=$1, description=$2, status=$3, doc_count=$4, updated_at=$5 WHERE id=$6`
	args := []interface{}{ds.Name, ds.Description, ds.Status, ds.DocCount, ds.UpdatedAt, ds.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $7 AND tenant_id = $8`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) DeleteDataset(ctx context.Context, id string) error {
	query := `DELETE FROM datasets WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// --- Document CRUD (RAG) ---

func (r *Repository) CreateDocument(ctx context.Context, doc *Document) error {
	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}
	if doc.Status == "" {
		doc.Status = "processing"
	}
	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO documents (id, dataset_id, org_id, tenant_id, name, source, chunk_count, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		doc.ID, doc.DatasetID, doc.OrgID, doc.TenantID, doc.Name, doc.Source, doc.ChunkCount, doc.Status, doc.CreatedAt, doc.UpdatedAt)
	return err
}

func (r *Repository) GetDocument(ctx context.Context, id string) (*Document, error) {
	doc := &Document{}
	query := `SELECT id, dataset_id, org_id, tenant_id, name, source, chunk_count, status, created_at, updated_at
		 FROM documents WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&doc.ID, &doc.DatasetID, &doc.OrgID, &doc.TenantID, &doc.Name, &doc.Source, &doc.ChunkCount, &doc.Status, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return doc, err
}

func (r *Repository) ListDocuments(ctx context.Context, datasetID string) ([]*Document, error) {
	query := `SELECT id, dataset_id, org_id, tenant_id, name, source, chunk_count, status, created_at, updated_at
		 FROM documents WHERE dataset_id = $1`
	args := []interface{}{datasetID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []*Document
	for rows.Next() {
		doc := &Document{}
		if err := rows.Scan(&doc.ID, &doc.DatasetID, &doc.OrgID, &doc.TenantID, &doc.Name, &doc.Source, &doc.ChunkCount, &doc.Status, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (r *Repository) UpdateDocument(ctx context.Context, doc *Document) error {
	doc.UpdatedAt = time.Now()
	query := `UPDATE documents SET name=$1, source=$2, chunk_count=$3, status=$4, updated_at=$5 WHERE id=$6`
	args := []interface{}{doc.Name, doc.Source, doc.ChunkCount, doc.Status, doc.UpdatedAt, doc.ID}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $7 AND tenant_id = $8`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *Repository) DeleteDocument(ctx context.Context, id string) error {
	query := `DELETE FROM documents WHERE id = $1`
	args := []interface{}{id}
	if scope := scopeFromContext(ctx); scope != nil {
		query += ` AND org_id = $2 AND tenant_id = $3`
		args = append(args, scope.OrgID, scope.TenantID)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// =============================================================================
// MemoryRepository — 内存实现（用于测试）
type scopeInfo struct {
	OrgID    string
	TenantID string
}

func scopeFromContext(ctx context.Context) *scopeInfo {
	orgID, tenantID, ok := port.RepoScopeFrom(ctx)
	if !ok {
		return nil
	}
	return &scopeInfo{OrgID: orgID, TenantID: tenantID}
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
