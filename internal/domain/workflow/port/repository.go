package port

import "context"

// Repository 工作流存储接口
type Repository interface {
	// Workflow CRUD
	CreateWorkflow(ctx context.Context, w *Workflow) error
	GetWorkflow(ctx context.Context, id string) (*Workflow, error)
	UpdateWorkflow(ctx context.Context, w *Workflow) error
	DeleteWorkflow(ctx context.Context, id string) error
	ListWorkflows(ctx context.Context, params ListWorkflowsParams) (*ListWorkflowsResult, error)

	// Organization CRUD
	CreateOrganization(ctx context.Context, org *Organization) error
	GetOrganization(ctx context.Context, id string) (*Organization, error)
	ListOrganizations(ctx context.Context) ([]*Organization, error)
	UpdateOrganization(ctx context.Context, org *Organization) error
	DeleteOrganization(ctx context.Context, id string) error

	// Tenant CRUD
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, id string) (*Tenant, error)
	ListTenants(ctx context.Context, orgID string) ([]*Tenant, error)
	UpdateTenant(ctx context.Context, tenant *Tenant) error
	DeleteTenant(ctx context.Context, id string) error

	// Dataset CRUD (RAG)
	CreateDataset(ctx context.Context, ds *Dataset) error
	GetDataset(ctx context.Context, id string) (*Dataset, error)
	ListDatasets(ctx context.Context, orgID, tenantID string) ([]*Dataset, error)
	UpdateDataset(ctx context.Context, ds *Dataset) error
	DeleteDataset(ctx context.Context, id string) error

	// Document CRUD (RAG)
	CreateDocument(ctx context.Context, doc *Document) error
	GetDocument(ctx context.Context, id string) (*Document, error)
	ListDocuments(ctx context.Context, datasetID string) ([]*Document, error)
	UpdateDocument(ctx context.Context, doc *Document) error
	DeleteDocument(ctx context.Context, id string) error

	// WorkflowRun CRUD
	CreateRun(ctx context.Context, run *WorkflowRun) error
	GetRun(ctx context.Context, id string) (*WorkflowRun, error)
	UpdateRun(ctx context.Context, run *WorkflowRun) error
	ListRuns(ctx context.Context, workflowID string, page, pageSize int) ([]*WorkflowRun, error)

	// NodeExecution 独立表
	BatchCreateNodeExecs(ctx context.Context, records []*NodeExecutionRecord) error
	ListNodeExecsByRunID(ctx context.Context, runID string) ([]*NodeExecutionRecord, error)

	// ConversationTrace — AI Provider 调用溯源（旧接口，兼容保留）
	AppendTrace(ctx context.Context, conversationID string, trace *LLMCallTrace) error
	GetTrace(ctx context.Context, conversationID string) (*ConversationTrace, error)

	// LLMCallTrace 独立表（新接口）
	CreateLLMTrace(ctx context.Context, record *LLMCallTraceRecord) error
	ListLLMTraces(ctx context.Context, conversationID string) ([]*LLMCallTraceRecord, error)

	// 会话归属校验
	EnsureConversationOwnership(ctx context.Context, conversationID, orgID, tenantID string) error
	ValidateConversationOwnership(ctx context.Context, conversationID, orgID, tenantID string) error

	// 多租户迁移
	EnsureTenantTables(ctx context.Context) error
	EnsureTenantColumns(ctx context.Context) error
	EnsureRAGTables(ctx context.Context) error
}

// scopeInfo 用于从 context 中读取 scope（repository 层的轻量读取）
type scopeInfo struct {
	OrgID    string
	TenantID string
}

type repoScopeKey struct{}

// WithRepoScope 注入 scope 到 context（供 repository 层使用）
func WithRepoScope(ctx context.Context, orgID, tenantID string) context.Context {
	return context.WithValue(ctx, repoScopeKey{}, &scopeInfo{OrgID: orgID, TenantID: tenantID})
}

// RepoScopeFrom 从 context 读取 repository scope（供其他包复用）
func RepoScopeFrom(ctx context.Context) (orgID, tenantID string, ok bool) {
	scope := scopeFromContext(ctx)
	if scope == nil {
		return "", "", false
	}
	return scope.OrgID, scope.TenantID, true
}

func scopeFromContext(ctx context.Context) *scopeInfo {
	if scope, ok := ctx.Value(repoScopeKey{}).(*scopeInfo); ok {
		return scope
	}
	return nil
}
