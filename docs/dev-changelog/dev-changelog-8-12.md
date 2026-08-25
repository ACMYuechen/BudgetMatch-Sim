# 8 月 12 日开发记录

## Agent Conversation/Turn 长期记忆

- 推荐 RPC 移除可伪造的 `user_id` 入参，统一使用认证拦截器身份；HTTP 网关与 SSE 均需登录。
- 增加 `turn_id` 幂等语义，重复请求直接恢复已保存结果。
- 将 PostgreSQL 记忆模型重构为 `agent_conversations` 与 `agent_conversation_turns`，保存最新结构化约束和完整轮次结果；数据库尚未部署，不保留旧消息表兼容迁移。
- PostgreSQL 使用 advisory lock，Redis 使用分布式锁，InMemory 使用服务内锁，保证同一会话执行与删除顺序。
- 增加会话列表、轮次历史和删除 API，并实现前端会话工作区、URL 恢复与跨轮追问。
- SSE 补齐 Bearer Token、非 2xx 错误解析和 401 登录失效处理。
- `MaxContextTokens` 改为系统提示、当前请求和历史消息的严格近似总上限；当前请求超限时返回可修正的 400 错误。

## 验证

- Go Agent 相关单元测试通过。
- 前端 TypeScript、ESLint 和 Vite production build 通过。
- PostgreSQL 集成测试支持 `AGENT_MEMORY_TEST_PG_DSN`，也复用 CI 的 `RAG_TEST_PG_DSN`；未提供 DSN 时跳过真实数据库用例。
