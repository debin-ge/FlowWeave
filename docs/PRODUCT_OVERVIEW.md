# FlowWeave 产品介绍

## 一句话介绍

FlowWeave 是一个面向企业 AI 应用的工作流运行时，让 LLM 编排、记忆管理、检索增强和多租户隔离在同一套后端能力中落地。

## 解决什么问题

当 AI 应用从 Demo 走向生产，团队通常会遇到：

- 工作流逻辑散落在业务代码里，难维护、难复用
- 对话越长成本越高，效果却不稳定
- 多租户场景下，数据边界和会话归属难保证
- 出问题时缺少可追溯链路，排障效率低

FlowWeave 通过“统一运行时”方式，把这些问题收敛为标准能力。

## 核心价值

### 1. 更快交付

- 基于 DSL 编排工作流，降低业务接入门槛
- 同时支持同步执行与流式执行（SSE）
- 提供可直接复用的示例模板

### 2. 更稳运行

- 内置短期记忆、中期摘要、上下文压缩机制
- 支持长对话场景下的成本与效果平衡
- 关键运行数据统一落库，便于审计和回放

### 3. 更安全可控

- 多租户 scope 注入与隔离
- 会话归属校验，避免跨租户串话
- 可按组织/租户维度管理知识库与执行数据

### 4. 更易扩展

- 节点、工具、Provider 均采用可扩展抽象
- RAG 模块支持按需启用，不强耦合核心执行链路
- 支持从轻量起步，按业务复杂度逐步增强

## 关键能力版图

- Workflow Runtime：DSL 驱动的工作流执行引擎
- Memory Runtime：STM + MTM + Gateway 压缩
- Tenant Isolation：JWT scope + conversation ownership
- RAG Runtime（可选）：BM25 / HYBRID / AUTO 检索
- Observability：run / node execution / llm traces

## 典型应用场景

- 企业知识助手与内部问答系统
- 智能客服与多轮对话机器人
- Agent 工作流编排平台
- 需要租户隔离和可审计链路的 AI SaaS 后端

## 为什么选择 FlowWeave

- 不只是“调用模型”，而是提供完整 AI 运行时能力
- 不只关注效果，也关注成本、治理与上线可行性
- 不只适合原型，也适合持续迭代的工程环境

## 快速体验

```bash
cp .env.example .env
psql "$DATABASE_URL" -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"
psql "$DATABASE_URL" -f migrations/postgres/schema.sql
go run ./cmd/server
curl http://localhost:8080/health
```

## 进一步阅读

- [项目架构文档](./ARCHITECTURE.md)
- [功能文档](./FEATURES.md)
- [使用文档](./USAGE.md)
- [部署文档](./DEPLOYMENT.md)
