# FlowWeave 部署文档

## 1. 部署目标

本文给出 FlowWeave 的基础部署方式，适合测试环境与中小规模生产环境起步。

## 2. 依赖清单

必需依赖：

- PostgreSQL（建议 14+）
- Redis（建议 6+）

可选依赖：

- OpenSearch（启用 RAG 时）

应用运行环境：

- Linux / macOS
- Go 1.25+（源码运行时）

## 3. 配置建议

## 3.1 必填配置

- `DATABASE_URL`
- `REDIS_URL`

## 3.2 生产建议配置

- `JWT_SECRET`：必须设置强随机值
- `JWT_ISSUER`：建议开启 issuer 校验
- `LOG_FORMAT=json`：便于日志平台采集
- `OPENAI_API_KEY`：使用密钥管理系统注入，不要写死在仓库

## 3.3 RAG 相关（按需）

- `OPENSEARCH_URL`
- `OPENSEARCH_USERNAME`
- `OPENSEARCH_PASSWORD`
- `RAG_DEFAULT_MODE`
- `RAG_EMBEDDING_*`
- `RAG_RERANK_*`

## 4. 数据库初始化

首次部署建议执行：

```bash
psql "$DATABASE_URL" -c "CREATE EXTENSION IF NOT EXISTS pgcrypto;"
psql "$DATABASE_URL" -f migrations/postgres/schema.sql
```

说明：

- 应用启动阶段会自动执行一部分 schema 兼容修复
- 但不应替代正式的数据库变更流程

## 5. 启动方式

## 5.1 直接运行

```bash
go run ./cmd/server
```

## 5.2 编译后运行

```bash
go build -o bin/flowweave ./cmd/server
./bin/flowweave
```

## 6. 上线前检查清单

- 健康检查 `GET /health` 可用
- PostgreSQL 连通、建表成功
- Redis 连通可写
- JWT 已开启并验证通过
- 至少一次工作流执行成功
- 日志可采集
- 如启用 RAG，OpenSearch 索引创建成功

## 7. 生产运行建议

- 在反向代理层设置超时、限流与请求体大小限制
- 配置 API 网关或 WAF 做访问控制
- 使用独立数据库账号并最小权限授权
- 对关键数据表定期备份
- 对 `workflow_runs` / `node_executions` / `llm_call_traces` 设计归档策略

## 8. 监控建议

建议至少接入以下指标：

- 请求成功率与延迟（按路由）
- 工作流执行成功率
- 节点失败率
- LLM 调用耗时与失败率
- Redis / PostgreSQL 连接池状态
- OpenSearch 检索耗时与错误率（启用 RAG 时）

## 9. 安全注意事项

- 不要在日志中输出敏感密钥
- 生产环境避免使用弱 `JWT_SECRET`
- OpenSearch TLS/证书策略需按生产标准配置
- 数据库连接建议开启 SSL

## 10. 扩展方向

- 容器化与编排（Docker/Kubernetes）
- 统一配置中心与密钥管理
- 灰度发布与回滚机制
- 全链路可观测（Tracing + Metrics + Logs）
