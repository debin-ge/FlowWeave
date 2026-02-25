# FlowWeave 部署文档（Docker Compose）

## 1. 部署方式

FlowWeave 采用 Docker Compose 作为默认部署方式，一次性拉起：

- FlowWeave 应用服务
- PostgreSQL
- Redis
- OpenSearch

## 2. 目录与文件说明

核心文件：

- `docker-compose.yml`
- `Dockerfile`
- `.env.example`
- `migrations/postgres/00-enable-extension.sql`
- `migrations/postgres/schema.sql`
- `docker/opensearch/Dockerfile`

## 3. 环境变量注入

应用容器使用 `env_file` 注入：

- `docker-compose.yml` -> `flowweave.env_file` -> `./.env`

初始化步骤：

```bash
cp .env.example .env
```

至少修改以下变量：

- `OPENAI_API_KEY`
- `JWT_SECRET`（建议生产必须设置）
- `OPENSEARCH_IK_PLUGIN_URL`（必须，安装中文 IK 插件）

## 4. 关键参数（Compose 默认）

### 4.1 Postgres

- 数据库名：`flowweave`
- 用户名：`flowweave`
- 密码：`flowweave`
- 对外端口：`5432`
- 数据卷：`pg-data`

### 4.2 Redis

- 对外端口：`6379`
- 持久化：AOF 开启
- 数据卷：`redis-data`

### 4.3 OpenSearch

- 对外端口：`9200`
- 管理端口：`9600`
- 单节点模式：`discovery.type=single-node`
- 默认关闭 security plugin（便于本地和测试环境）
- 构建时安装 IK 中文分词插件（来自 `OPENSEARCH_IK_PLUGIN_URL`）
- 数据卷：`opensearch-data`

### 4.4 FlowWeave App

- 对外端口：`8080`
- 启动依赖：Postgres/Redis 健康检查通过 + OpenSearch 已启动

## 5. 数据库初始化策略

PostgreSQL 首次启动时自动执行：

1. `migrations/postgres/00-enable-extension.sql`
   - 创建 `pgcrypto` 扩展
2. `migrations/postgres/schema.sql`
   - 创建业务表结构

说明：

- 仅在数据库目录首次初始化时自动执行
- 若已有旧数据卷，变更脚本不会自动重跑

## 6. 启动与验证

启动：

```bash
docker compose up -d --build
```

查看状态：

```bash
docker compose ps
```

查看日志：

```bash
docker compose logs -f flowweave
```

健康检查：

```bash
curl http://localhost:8080/health
```

## 7. 停止与清理

停止容器（保留数据）：

```bash
docker compose down
```

停止并清理数据卷（慎用）：

```bash
docker compose down -v
```

## 8. 生产环境建议

- 将 `OPENAI_API_KEY`、`JWT_SECRET` 交由密钥管理系统注入
- 不要在生产环境使用默认数据库密码
- OpenSearch 建议启用安全认证与 TLS
- 结合反向代理（Nginx/API Gateway）统一做限流与访问控制
- 为 `workflow_runs` / `node_executions` / `llm_call_traces` 制定归档策略

## 9. 常见问题

- 应用容器启动失败且提示数据库连通问题
  - 检查 `DATABASE_URL` 是否使用服务名 `postgres`
- RAG 接口不可用
  - 检查 `OPENSEARCH_URL` 是否为 `http://opensearch:9200`
  - 检查 `OPENSEARCH_IK_PLUGIN_URL` 是否可下载且版本匹配
- LLM 节点不可用
  - 检查 `OPENAI_API_KEY` 是否正确注入
- 想重新执行数据库初始化脚本
  - 先 `docker compose down -v` 清空数据卷，再重新 `up`
