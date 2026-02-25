# FlowWeave

FlowWeave 是一个面向 AI 应用的工作流运行时（Workflow Runtime）。
它聚焦在一件事：把“LLM + 记忆 + 检索 + 多租户隔离”这套能力，变成可编排、可追踪、可部署的后端基础设施。

## 项目定位

在真实业务里，AI 应用通常会遇到这些问题：
- 对话变长后，上下文成本失控，效果下降
- 多租户数据边界不清晰，难以安全落地
- 检索、记忆、调用链路分散，排查成本高
- 原型能跑，但很难变成稳定的服务

FlowWeave 的目标是提供一套工程化运行时，让你把 AI 能力从 Demo 快速推进到可持续迭代的服务形态。

## 核心功能

### 1. 工作流编排与执行

- 基于 DSL 的工作流定义与运行
- 支持同步执行与流式执行（SSE）
- 内置常见节点类型（如 `start`、`llm`、`if-else`、`template`、`http-request`、`iteration`、`end`）

### 2. 三层记忆体系

- STM（短期记忆）：会话窗口级上下文保留
- MTM（中期记忆）：按阈值生成摘要，降低长对话成本
- Gateway 压缩：基于 Token 预算触发压缩，尽量保留关键信息

### 3. 多租户隔离

- 通过 JWT 注入 `org_id` / `tenant_id` 作用域
- 会话 `conversation_id` 归属校验，防止跨租户串话
- 工作流、运行记录、知识库等数据按租户边界组织

### 4. RAG 检索能力（可选）

- 支持 `BM25 / HYBRID / AUTO` 检索模式
- 支持数据集、文档管理与文件上传解析
- 可选 Embedding 与 Rerank，按效果与成本做取舍

### 5. 可观测与可追溯

- 运行记录（Run）
- 节点执行明细（Node Executions）
- LLM 调用溯源（Traces）

## 项目优势

- **工程化优先**：基于 Go + PostgreSQL + Redis，偏向稳定可维护的服务化实现
- **长对话友好**：记忆与压缩机制内建，不需要每个业务重复造轮子
- **多租户可落地**：把租户隔离当作一等能力，而不是后补逻辑
- **可扩展设计**：检索、记忆、节点能力都可按业务逐步增强
- **成本与效果平衡**：默认可低成本运行，按需开启更高效果能力

## 适用场景

- 企业内部 AI 助手（需要租户隔离与审计）
- 带上下文记忆的客服/问答系统
- 知识库问答与 Agent 化工作流
- 需要可追踪执行链路的 AI 后端服务

## 架构概览

```text
Client / API
   |
FlowWeave Server
   |-- Workflow Engine (DSL, Sync/SSE)
   |-- Memory Coordinator (STM / MTM / Gateway)
   |-- RAG (Retriever / Indexer, optional)
   |
Storage Layer
   |-- PostgreSQL (workflow/run/trace/tenant)
   |-- Redis (stm/cache/lock)
   |-- OpenSearch (optional)
```

## 快速体验（简版）

默认使用 Docker Compose 一键拉起 `FlowWeave + PostgreSQL + Redis + OpenSearch`。

1. 准备应用环境变量：

```bash
cp .env.example .env
```

2. 在 `.env` 中至少设置：
- `OPENAI_API_KEY`
- `JWT_SECRET`（建议）
- `OPENSEARCH_IK_PLUGIN_URL`（中文分词插件下载地址）

3. 启动全套服务：

```bash
docker compose up -d --build
```

4. 验证服务：

```bash
curl http://localhost:8080/health
```

## 使用入口

- 工作流示例：[`examples/`](examples)
- 典型接口：
  - `POST /api/v1/workflows`
  - `POST /api/v1/workflows/{id}/run`
  - `POST /api/v1/workflows/{id}/run/stream`
  - `POST /rag/search`（启用 RAG 时）

## 补充说明

- 未配置 `JWT_SECRET` 时，服务以开发兼容模式运行（不强制鉴权）
- 未配置或无法连接 OpenSearch 时，RAG 功能自动降级为不可用，不影响核心工作流能力
- 为支持中文检索，OpenSearch 容器构建时会安装 IK 插件（需配置 `OPENSEARCH_IK_PLUGIN_URL`）
- Compose 默认会自动执行 `migrations/postgres/` 下的 PostgreSQL 初始化脚本
- 配置模板请查看 [`.env.example`](.env.example)
