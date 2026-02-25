# FlowWeave 使用文档

## 1. 环境准备

默认使用 Docker Compose 运行，需准备：

- Docker
- Docker Compose（`docker compose`）

## 2. 初始化配置

复制 Docker 环境变量模板：

```bash
cp .env.example .env
```

最小必填项：

- `OPENAI_API_KEY`（要运行 LLM 节点必须配置）
- `OPENSEARCH_IK_PLUGIN_URL`（用于安装 OpenSearch 中文 IK 插件）

OpenSearch 2.12+ 额外说明：

- 本地开发（默认）可保持：
  - `OPENSEARCH_SECURITY_DISABLED=true`
  - `OPENSEARCH_DISABLE_DEMO_CONFIG=true`
- 若你要启用安全插件（`OPENSEARCH_SECURITY_DISABLED=false`），必须设置：
  - `OPENSEARCH_INITIAL_ADMIN_PASSWORD`（强密码）

常用建议项：

- `JWT_SECRET`（生产建议开启）

说明：

- 通过 Compose 启动时，PostgreSQL 会自动执行初始化脚本
- `migrations/postgres/schema.sql` 会在数据库首次启动时自动导入
- OpenSearch 镜像构建阶段会安装 IK 插件，用于 `ik_max_word` 中文分词
- `.env` 中同时包含 Compose 服务参数（如 `POSTGRES_*`、`REDIS_*`、`OPENSEARCH_*`）和应用参数
- RAG 运行参数（如 `RAG_EMBEDDING_HTTP_TIMEOUT`、`RAG_EMBEDDING_BATCH_SIZE`、`RAG_CACHE_WRITE_TIMEOUT`）也可在 `.env` 中直接调优

## 3. 启动全套服务

```bash
docker compose up -d --build
```

查看启动日志：

```bash
docker compose logs -f flowweave
```

## 4. 健康检查与停止服务

```bash
curl http://localhost:8080/health
```

停止并清理容器（保留数据卷）：

```bash
docker compose down
```

连同数据卷一并清理：

```bash
docker compose down -v
```

## 5. 快速创建并运行工作流

## 5.1 创建工作流

```bash
DSL_JSON=$(cat examples/chatbot_with_memory.json)
curl -sS -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"chatbot-demo\",\"description\":\"memory chatbot\",\"dsl\":$DSL_JSON}"
```

返回中的 `data.id` 即 `workflow_id`。

## 5.2 同步运行

```bash
curl -sS -X POST http://localhost:8080/api/v1/workflows/{workflow_id}/run \
  -H "Content-Type: application/json" \
  -d '{
    "inputs": { "query": "你好，我叫小明" },
    "conversation_id": "conv-001"
  }'
```

## 5.3 流式运行（SSE）

```bash
curl -N -X POST http://localhost:8080/api/v1/workflows/{workflow_id}/run/stream \
  -H "Content-Type: application/json" \
  -d '{
    "inputs": { "query": "你还记得我叫什么吗？" },
    "conversation_id": "conv-001"
  }'
```

SSE 事件类型：

- `message`：过程事件
- `done`：结束事件
- `error`：执行失败事件

## 6. 鉴权使用（JWT）

当配置 `JWT_SECRET` 后，业务接口需要：

```http
Authorization: Bearer <token>
```

Token claims 至少包含：

```json
{
  "sub": "user-123",
  "org_id": "your-org-uuid",
  "tenant_id": "your-tenant-uuid",
  "roles": ["admin"]
}
```

常见错误：

- `401 unauthorized`：token 格式或签名不正确
- `403 forbidden_scope`：缺失 `org_id` 或 `tenant_id`
- `409 conversation_id_conflict`：会话 ID 归属冲突

## 7. RAG 使用（可选）

前提：

- 配置好 `OPENSEARCH_URL` 等参数
- 服务日志出现 RAG 初始化成功信息

## 7.1 创建数据集

```bash
curl -sS -X POST http://localhost:8080/rag/datasets/ \
  -H "Content-Type: application/json" \
  -d '{
    "name": "知识库A",
    "description": "用于演示"
  }'
```

## 7.2 上传文档

```bash
curl -sS -X POST http://localhost:8080/rag/datasets/{dataset_id}/documents/upload \
  -F "file=@./your-doc.md"
```

## 7.3 检索

```bash
curl -sS -X POST http://localhost:8080/rag/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "什么是工作流引擎",
    "mode": "BM25",
    "top_k": 5
  }'
```

## 8. 常用接口速查

工作流：

- `POST /api/v1/workflows`
- `GET /api/v1/workflows`
- `GET /api/v1/workflows/{id}`
- `PUT /api/v1/workflows/{id}`
- `DELETE /api/v1/workflows/{id}`
- `POST /api/v1/workflows/{id}/run`
- `POST /api/v1/workflows/{id}/run/stream`
- `GET /api/v1/runs/{id}`
- `GET /api/v1/runs/{id}/nodes`
- `GET /api/v1/traces/{conversation_id}`

组织租户：

- `POST /organizations/`
- `GET /organizations/`
- `POST /tenants/`
- `GET /tenants/`

RAG：

- `POST /rag/search`
- `POST /rag/datasets/`
- `GET /rag/datasets/`
- `POST /rag/datasets/{datasetID}/documents/`
- `POST /rag/datasets/{datasetID}/documents/upload`
- `POST /rag/datasets/{datasetID}/documents/qa`

## 9. 故障排查

- 启动失败提示 `DATABASE_URL is required` 或 `REDIS_URL is required`
  - 检查 `.env` 是否生效，变量是否为空
- LLM 节点报 provider 相关错误
  - 检查 `OPENAI_API_KEY` 与 `OPENAI_BASE_URL`
- RAG 不可用
  - 检查 OpenSearch 连通性、认证信息、索引权限
- 会话跨租户无法访问
  - 预期行为，需使用正确 `org_id` 与 `tenant_id`
