-- 10) external_async_tasks 统一外部异步任务表
CREATE TABLE IF NOT EXISTS external_async_tasks (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_type            VARCHAR(64) NOT NULL,
    provider             VARCHAR(64) NOT NULL,
    provider_task_ref    VARCHAR(256) NOT NULL,
    run_id               UUID REFERENCES workflow_runs(id) ON DELETE SET NULL,
    workflow_id          UUID REFERENCES workflows(id) ON DELETE SET NULL,
    node_id              VARCHAR(255) DEFAULT '',
    org_id               UUID,
    tenant_id            UUID,
    conversation_id      VARCHAR(255) DEFAULT '',
    status               VARCHAR(32) NOT NULL DEFAULT 'submitted',
    submitted_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    started_at           TIMESTAMP WITH TIME ZONE,
    completed_at         TIMESTAMP WITH TIME ZONE,
    callback_received_at TIMESTAMP WITH TIME ZONE,
    next_poll_at         TIMESTAMP WITH TIME ZONE,
    poll_count           INTEGER NOT NULL DEFAULT 0,
    callback_mode        VARCHAR(32) NOT NULL DEFAULT 'none',
    callback_token       VARCHAR(128) DEFAULT '',
    callback_url         VARCHAR(1024) DEFAULT '',
    submit_payload       JSONB,
    query_payload        JSONB,
    result_payload       JSONB,
    normalized_result    JSONB,
    result_url           VARCHAR(2048) DEFAULT '',
    error_code           VARCHAR(128) DEFAULT '',
    error_message        TEXT DEFAULT '',
    version              BIGINT NOT NULL DEFAULT 1,
    created_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_external_async_tasks_provider_ref ON external_async_tasks(provider, provider_task_ref);
CREATE UNIQUE INDEX IF NOT EXISTS uk_external_async_tasks_callback_token ON external_async_tasks(callback_token) WHERE callback_token <> '';
CREATE INDEX IF NOT EXISTS idx_external_async_tasks_status_next_poll ON external_async_tasks(status, next_poll_at);
CREATE INDEX IF NOT EXISTS idx_external_async_tasks_scope_created ON external_async_tasks(org_id, tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_external_async_tasks_run ON external_async_tasks(run_id);
CREATE INDEX IF NOT EXISTS idx_external_async_tasks_workflow_node ON external_async_tasks(workflow_id, node_id);
CREATE INDEX IF NOT EXISTS idx_external_async_tasks_provider_updated ON external_async_tasks(provider, updated_at DESC);
