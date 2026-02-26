# FlowWeave 功能文档

## 1. 功能总览

FlowWeave 当前版本围绕 5 个能力域构建：

- 工作流编排与执行
- 记忆管理
- 多租户隔离
- RAG 检索（可选）
- 调用链路可观测

## 2. 工作流能力

## 2.1 工作流管理

支持：

- 创建工作流
- 查询工作流
- 更新工作流
- 删除工作流
- 分页列表查询

工作流定义格式：

- 以 JSON DSL 存储
- 核心结构：`nodes` + `edges`

## 2.2 运行模式

支持：

- 同步运行（阻塞返回最终输出）
- 流式运行（SSE 持续返回事件）

运行结果能力：

- 最终输出 `outputs`
- 运行状态 `running/succeeded/failed/aborted`
- 节点执行明细记录

## 2.3 当前已注册节点类型

- `start`
- `llm`
- `if-else`
- `template`
- `http-request`
- `code`
- `iteration`
- `answer`
- `end`

## 3. 记忆能力

## 3.1 STM（短期记忆）

能力：

- 会话级历史消息保留
- 支持窗口大小配置
- 存储介质为 Redis

用途：

- 保持短轮次上下文连续性

## 3.2 MTM（中期记忆）

能力：

- 达到阈值后对会话历史生成摘要
- 摘要持久化到 PostgreSQL
- 支持 Redis 缓存加速读取

用途：

- 降低长对话 token 成本
- 提升长期上下文稳定性

## 3.3 Gateway 压缩

能力：

- 基于上下文窗口与阈值比例触发压缩
- 可提取关键事实（key facts）
- 使用独立压缩模型配置

用途：

- 避免上下文无限增长
- 在成本与信息保留之间做平衡

## 4. 多租户能力

## 4.1 鉴权与 scope 注入

能力：

- JWT 鉴权
- 从 token 提取 `org_id` / `tenant_id`
- 作用域注入请求上下文

## 4.2 数据隔离

能力：

- Repository 层按 scope 过滤数据
- 工作流、运行、记忆、知识库按租户边界组织

## 4.3 会话归属保护

能力：

- `conversation_id` 注册归属
- 读写时校验归属一致性
- 冲突时返回 `conversation_id_conflict`

## 5. RAG 能力（可选）

## 5.1 检索能力

支持模式：

- `BM25`
- `HYBRID`
- `AUTO`

支持能力：

- Top-K 检索
- 数据集过滤
- score threshold 过滤
- 可选 rerank

## 5.2 索引与知识库管理

支持：

- Dataset CRUD
- Document CRUD
- 文本内容直接入库
- QA 对批量入库
- 文件上传解析入库

## 5.3 文件解析支持格式

- Markdown: `.md`, `.markdown`
- 文本类: `.txt`, `.text`, `.csv`, `.log`, `.json`, `.xml`, `.yaml`, `.yml`
- Office/PDF: `.pdf`, `.docx`

## 6. 可观测能力

## 6.1 运行可观测

- `workflow_runs` 保存每次运行状态与耗时
- `node_executions` 保存节点级执行明细

## 6.2 LLM 调用溯源

- 记录 provider/model/request/response/elapsed
- 同时维护兼容旧表和新独立表结构

## 7. 功能开关与降级行为

- 未配置 `JWT_SECRET`：服务启动失败（强制鉴权配置）
- 未配置 `OPENAI_API_KEY`：LLM Provider 不注册，LLM 节点不可用
- OpenSearch 不可用：RAG API 不启用，核心 workflow 能力可继续运行

## 8. 当前边界说明

- 生产级高可用（HA/自动伸缩）需在部署层补齐
- 监控与告警需接入外部平台（如 Prometheus/Grafana）
- OpenSearch TLS 当前默认开发友好配置，生产需强化安全策略
