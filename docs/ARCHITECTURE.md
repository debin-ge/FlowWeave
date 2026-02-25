# FlowWeave 项目架构文档

## 1. 架构目标

FlowWeave 的核心目标是提供一套面向 AI 应用的工程化运行时，重点解决四类问题：

- 工作流编排与执行的稳定性
- 长对话上下文的成本与质量平衡
- 多租户数据隔离与会话归属安全
- 检索、执行、溯源链路的一体化

## 2. 总体架构

```text
Client / API Gateway
        |
        v
FlowWeave HTTP Server (chi)
        |
        +-- API Layer
        |   - Workflow API
        |   - Tenant/Organization API
        |   - RAG API (optional)
        |
        +-- Application Layer
        |   - WorkflowRunner
        |   - Bootstrap (Node/Provider 注册)
        |
        +-- Domain Layer
        |   - Workflow Engine (Graph + Node Runtime)
        |   - Memory Coordinator (STM/MTM/Gateway)
        |   - RAG Retriever/Indexer
        |
        +-- Infrastructure Layer
            - PostgreSQL Repository
            - Redis Store/Cache/Lock
            - OpenSearch Client (optional)
```

## 3. 分层设计

### 3.1 API 层

职责：

- 提供 HTTP 接口与统一响应
- 处理鉴权（JWT）与 scope 注入
- 完成请求校验、执行触发、结果返回

关键实现：

- `internal/api/server.go`
- `internal/api/handler.go`
- `internal/api/auth_middleware.go`
- `internal/api/rag_handler.go`
- `internal/api/tenant_handler.go`

### 3.2 应用层

职责：

- 从 DSL 构建可运行图
- 注入上下文能力（记忆、RAG scope、工具注册表）
- 对外提供同步执行/事件流执行入口

关键实现：

- `internal/app/workflow/runner.go`
- `internal/app/bootstrap/node_registry.go`
- `internal/app/bootstrap/provider_registry.go`

### 3.3 领域层

Workflow 子域：

- 图构建：`graph.Init`
- 运行时状态：`GraphRuntimeState + VariablePool`
- 执行引擎：队列驱动并发执行 + 事件分发

Memory 子域：

- STM：短期会话消息窗口
- MTM：摘要化中期记忆
- Gateway：Token 预算触发压缩 + key facts

RAG 子域：

- 检索模式：BM25/HYBRID/AUTO
- 索引流程：解析 -> 分块 -> 可选向量 -> OpenSearch

## 4. 核心运行链路

## 4.1 工作流执行链路

1. API 接收执行请求（同步或流式）
2. 校验并加载工作流 DSL
3. 创建 `workflow_runs` 记录
4. `WorkflowRunner` 构建 Graph + Runtime State
5. `GraphEngine` 并发执行节点，产出事件流
6. 汇总 outputs / node executions
7. 异步写回 run 状态、节点明细、LLM traces

## 4.2 记忆链路

1. LLM 节点读取当前会话记忆（STM/MTM/Gateway）
2. 将召回内容拼接到 prompt 消息
3. 生成响应后写入 STM
4. 达到摘要阈值时异步生成 MTM
5. 达到压缩阈值时执行 Gateway 压缩

## 4.3 多租户隔离链路

1. JWT 中间件解析 `org_id` / `tenant_id`
2. Scope 注入 request context
3. Repository 层通过 scope 条件过滤读写
4. `conversation_id` 在 `conversations` 表登记归属
5. 跨租户复用同一 `conversation_id` 时返回冲突

## 5. 数据存储架构

## 5.1 PostgreSQL

主要承载：

- 工作流定义：`workflows`
- 运行记录：`workflow_runs`
- 节点明细：`node_executions`
- 调用溯源：`conversation_traces`（兼容）/`llm_call_traces`（新）
- 组织租户：`organizations` / `tenants` / `conversations`
- 记忆摘要：`conversation_summaries`
- RAG 元数据：`datasets` / `documents`

初始化方式：

- 首次建议执行 `migrations/postgres/schema.sql`
- 服务启动时会执行若干 `Ensure*` 兼容建表/加列逻辑

## 5.2 Redis

主要承载：

- STM 状态存储
- MTM 缓存
- Gateway 压缩互斥锁
- RAG 查询缓存（可选）

## 5.3 OpenSearch（可选）

主要承载：

- RAG 分块索引（文本与可选向量）
- BM25 / Hybrid 检索

当 OpenSearch 未配置或不可达时：

- RAG 功能禁用
- 工作流与记忆能力不受影响

## 6. 可扩展点

## 6.1 节点扩展

- 新增节点实现 `node.Node` 接口
- 在 bootstrap 注册即可被 DSL 使用

## 6.2 Provider 扩展

- 对接新 LLM Provider，统一走 `provider.Provider` 抽象
- 当前内置 openai 兼容实现

## 6.3 Tool 扩展

- LLM 节点支持 DSL 配置 tools
- 当前已接入 `knowledge_search` 工具

## 6.4 检索与解析扩展

- 可新增 Parser 支持更多文件格式
- 可扩展检索策略、重排策略

## 7. 架构特性总结

- 以“运行时稳定性 + 业务可扩展性”为核心
- 通过分层与抽象减少业务耦合
- 将多租户、记忆、检索与溯源纳入统一运行时体系
