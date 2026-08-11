# Agent 多轮会话调用

推荐接口使用 conversation_id 关联同一用户的多轮上下文。首次请求可以不传该字段，服务端会生成 UUID 并在响应中稳定返回；后续请求应原样携带该值。

HTTP 网关从已认证的请求上下文读取用户身份，并将其传给 Agent RPC。客户端不传、也不能指定 user_id；相同的 conversation_id 在不同用户下会映射为彼此隔离的记忆。

## 首次调用

    POST /api/agent/recommend
    {
      "query": "预算 3000 元，想配一套学习桌面",
      "budget_cents": 300000,
      "max_items": 3
    }

响应中的 conversation_id 与 conversation_title 应由客户端保存：

    {
      "conversation_id": "4f73fd97-7a89-41ae-90cb-4a9e27de941c",
      "conversation_title": "预算 3000 元，想配一套学习桌面",
      "summary": "..."
    }

## 继续同一会话

    POST /api/agent/recommend
    {
      "query": "预算增加到 5000 元，优先考虑机械键盘",
      "budget_cents": 500000,
      "max_items": 4,
      "conversation_id": "4f73fd97-7a89-41ae-90cb-4a9e27de941c"
    }

服务会读取该用户此会话最近的历史偏好，并继续返回同一个 conversation_id 和首次生成的 conversation_title。

## 记忆边界

Memory.MaxHistory 控制每次读取的最近消息窗口；Memory.MaxContextTokens 进一步限制发送给 LLM 的消息上下文近似 token 数，优先保留最近的完整问答轮次。PostgreSQL 会保留窗口之外的完整消息，不会因为窗口滚动而删除旧记录。

## PostgreSQL 持久化

配置 Database.DSN 后，agent-rpc 使用 PostgreSQL 作为会话记忆的长期数据源，并与 RAG 复用同一个连接池。Database.AutoMigrate 为 true 时，服务启动会幂等创建；关闭自动迁移时会检查所需表是否已由外部迁移创建，缺表会直接阻止服务启动：

- agent_conversations：以 user_id + conversation_id 为主键，保存稳定标题、缓存版本及会话时间；
- agent_conversation_messages：顺序保存完整消息 JSON，删除会话时级联清理。

PostgreSQL 会话不应用 Memory.TTL，因此服务重启或间隔数天后，只要认证用户和 conversation_id 保持一致，仍能读取最近上下文。Memory.TTL 只作用于 Redis 快照、独立 Redis 记忆和 InMemory 降级实现。

## PostgreSQL + Redis 两级记忆

同时配置 Database.DSN 与 CacheRedis 时，PostgreSQL 是唯一持久化事实源，Redis 缓存最近 MaxHistory 条消息、稳定标题和对应的 PostgreSQL 会话版本：

    写入：提交 PostgreSQL 事务 → 会话版本递增 → 删除 Redis 旧快照
    首次读取：查询 PostgreSQL 版本 → Redis 未命中 → 从 PostgreSQL 读取窗口 → 回填 Redis
    后续读取：查询 PostgreSQL 版本 → Redis 版本一致 → 直接使用 Redis 快照

如果数据库提交后 Redis 删除失败，下一次读取也会因版本不一致而回源 PostgreSQL，不会返回陈旧记忆。Redis 读取、写入或清理失败时会记录日志并直接使用 PostgreSQL，推荐请求和持久化结果不受缓存故障影响。

只配置 Database 时直接使用 PostgreSQL；只配置 CacheRedis 时使用 Redis 共享短期记忆；两者都未配置时使用 InMemory。Redis 不是第二份持久数据，也不会反向覆盖 PostgreSQL。

## 并发边界

同一 agent-rpc 实例内，相同 user_id 与 conversation_id 的请求会串行完成“读取历史、执行推荐、写回历史”全过程；不同用户或不同会话仍可并行。等待中的请求会响应超时或主动取消。多实例部署如需保持同样语义，还需要接入分布式会话锁或带序号的 turn_id。
