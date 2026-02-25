-- ============================================================
-- FlowWeave 多租户隔离 Schema
-- ============================================================

-- 1) 组织表
CREATE TABLE IF NOT EXISTS organizations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       VARCHAR(64) NOT NULL UNIQUE,
    name       VARCHAR(255) NOT NULL,
    status     VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- 2) 租户表
CREATE TABLE IF NOT EXISTS tenants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    code       VARCHAR(64) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    status     VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, code),
    UNIQUE (org_id, id)
);

-- 3) 会话归属注册表（conversation_id 全局唯一）
CREATE TABLE IF NOT EXISTS conversations (
    conversation_id VARCHAR(255) PRIMARY KEY,
    org_id          UUID NOT NULL,
    tenant_id       UUID NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (conversation_id, org_id, tenant_id),
    FOREIGN KEY (org_id, tenant_id) REFERENCES tenants(org_id, id)
);

CREATE INDEX IF NOT EXISTS idx_conversations_scope ON conversations(org_id, tenant_id);

-- 4) workflows 工作流定义表
CREATE TABLE IF NOT EXISTS workflows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID,
    tenant_id   UUID,
    name        VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    dsl_json    JSONB NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status);
CREATE INDEX IF NOT EXISTS idx_workflows_name ON workflows(name);
CREATE INDEX IF NOT EXISTS idx_workflows_updated_at ON workflows(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflows_scope_updated ON workflows(org_id, tenant_id, updated_at DESC);

-- 5) workflow_runs 执行记录表
CREATE TABLE IF NOT EXISTS workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    org_id          UUID,
    tenant_id       UUID,
    conversation_id VARCHAR(255),
    status          VARCHAR(32) NOT NULL DEFAULT 'running',
    inputs          JSONB,
    outputs         JSONB,
    error           TEXT DEFAULT '',
    total_tokens    INTEGER DEFAULT 0,
    total_steps     INTEGER DEFAULT 0,
    elapsed_ms      BIGINT DEFAULT 0,
    started_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow_id ON workflow_runs(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_started_at ON workflow_runs(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_conversation_id ON workflow_runs(conversation_id);
CREATE INDEX IF NOT EXISTS idx_runs_scope_started ON workflow_runs(org_id, tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_scope_conv ON workflow_runs(org_id, tenant_id, conversation_id);

-- 5a) node_executions 节点执行记录表（独立表）
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

-- 6) conversation_summaries 中期记忆摘要表
CREATE TABLE IF NOT EXISTS conversation_summaries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID,
    tenant_id       UUID,
    conversation_id VARCHAR(255) NOT NULL UNIQUE,
    summary         TEXT NOT NULL DEFAULT '',
    turns_covered   INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conv_summaries_conv_id ON conversation_summaries(conversation_id);
CREATE INDEX IF NOT EXISTS idx_summaries_scope_conv ON conversation_summaries(org_id, tenant_id, conversation_id);

-- 7) conversation_traces AI Provider 调用溯源表（旧表，兼容保留）
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
CREATE INDEX IF NOT EXISTS idx_conv_traces_updated_at ON conversation_traces(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_scope_conv ON conversation_traces(org_id, tenant_id, conversation_id);

-- 7a) llm_call_traces LLM 调用溯源独立表（新表）
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

-- 8) datasets 知识库表 (RAG)
CREATE TABLE IF NOT EXISTS datasets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL,
    tenant_id   UUID NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    doc_count   INTEGER DEFAULT 0,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    FOREIGN KEY (org_id, tenant_id) REFERENCES tenants(org_id, id)
);

CREATE INDEX IF NOT EXISTS idx_datasets_scope ON datasets(org_id, tenant_id);

-- 9) documents 文档元数据表 (RAG)
CREATE TABLE IF NOT EXISTS documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_id  UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    org_id      UUID NOT NULL,
    tenant_id   UUID NOT NULL,
    name        VARCHAR(255) NOT NULL,
    source      VARCHAR(512) DEFAULT '',
    chunk_count INTEGER DEFAULT 0,
    status      VARCHAR(32) DEFAULT 'processing',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_documents_dataset ON documents(dataset_id);
CREATE INDEX IF NOT EXISTS idx_documents_scope ON documents(org_id, tenant_id);
