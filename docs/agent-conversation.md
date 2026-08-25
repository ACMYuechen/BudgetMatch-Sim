# Agent 会话与长期记忆

推荐接口使用 `conversation_id` 关联同一认证用户的多轮上下文，并使用 `turn_id` 保证单轮请求幂等。首次请求可不传 `conversation_id`，服务端生成后返回；前端应为每次发送生成新的 `turn_id`，网络重试沿用原值。

RPC 不再接受 `user_id`。Agent RPC 只信任认证拦截器写入的用户身份；即使两个用户使用相同 `conversation_id`，数据也按 `user_id + conversation_id` 隔离。

## 调用示例

首次请求：

```http
POST /api/agent/recommend
Authorization: Bearer <token>
Content-Type: application/json

{
  "query": "预算 3000 元，想配一套学习桌面",
  "budget_cents": 300000,
  "max_items": 3,
  "turn_id": "1f2e1ad7-cb89-47bb-b64b-4f86f6e99077"
}
```

响应会返回稳定的会话与轮次标识：

```json
{
  "conversation_id": "4f73fd97-7a89-41ae-90cb-4a9e27de941c",
  "conversation_title": "预算 3000 元，想配一套学习桌面",
  "turn_id": "1f2e1ad7-cb89-47bb-b64b-4f86f6e99077",
  "summary": "..."
}
```

继续同一会话时只填写本轮需要覆盖的结构化约束。下面没有传 `max_items`，所以会继承上一轮的 3 件：

```json
{
  "query": "预算增加到 5000 元，优先机械键盘",
  "budget_cents": 500000,
  "conversation_id": "4f73fd97-7a89-41ae-90cb-4a9e27de941c",
  "turn_id": "c026238c-710f-45d6-952a-fbb44f3c761c"
}
```

重复提交相同 `turn_id` 且请求内容一致时，服务直接返回已保存的完整结果，不会再次调用模型或新增轮次。如果复用同一 `turn_id` 却修改 `query`、`budget_cents` 或 `max_items`，接口返回 HTTP 409，防止把旧结果误认为新请求的响应。

## 请求边界

- `query` 必填，去除空白后不得为空，原始长度最多 2000 个字符；
- `budget_cents` 范围为 0 到 100000000000，0 表示交给文本解析或继承上一轮；
- `max_items` 范围为 0 到 10，0 表示交给文本解析或继承上一轮；
- `conversation_id` 与 `turn_id` 最多 128 个字符，不允许首尾空格；首次请求可留空并由服务端生成。

HTTP 网关和 Agent 业务服务都会执行边界校验，因此直接 RPC 调用也不能绕过这些规则。

## 管理接口

- `GET /api/agent/conversations?page=1&page_size=20`：按最近更新时间倒序列出当前用户会话。
- `GET /api/agent/conversations/:conversation_id/turns?page=1&page_size=50`：恢复会话状态与完整轮次。
- `DELETE /api/agent/conversations/:conversation_id`：删除当前用户的会话和全部轮次。
- `POST /api/agent/recommend/stream`：与同步接口使用相同鉴权、`conversation_id` 和 `turn_id` 语义。

前端路由 `/recommend/:conversationId` 会在刷新或跨设备打开时重新加载服务端历史；未登录访问会先跳转登录页。

## PostgreSQL 数据模型

数据库尚未投入使用，因此本次直接采用新结构，不兼容旧的消息表：

- `agent_conversations`：复合主键为 `user_id + conversation_id`，保存稳定标题、最新结构化 `state`、版本、轮次数与时间；
- `agent_conversation_turns`：复合主键为 `user_id + conversation_id + turn_id`，保存轮次序号、原始请求、输入预算/件数、解析意图、完整推荐结果和完成时间；
- `user_id + conversation_id + sequence` 有唯一索引，删除会话时轮次通过复合外键级联清理。

PostgreSQL 模式不应用 `Memory.TTL`，因此服务重启或间隔数天后仍能恢复。只配置 Redis 或未配置外部存储时仍是短期记忆，分别受 Redis TTL 或进程生命周期约束。

## 两级缓存

同时配置 `Database.DSN` 与 `CacheRedis` 时，PostgreSQL 是唯一事实源，Redis 只缓存最近 `MaxHistory` 条消息快照：

```text
写入：同会话锁 → PostgreSQL 事务保存会话与轮次 → 版本递增 → Redis 快照失效
读取：查询 PostgreSQL 版本 → 命中同版本 Redis 快照；否则回源并回填
```

Redis 故障只降低缓存命中率，不覆盖 PostgreSQL。只配置 Database 时直接读写 PostgreSQL；只配置 CacheRedis 时使用 Redis 短期会话；均未配置时使用 InMemory。

## 状态、上下文与并发边界

- 预算、最多件数、关键词和偏好以结构化状态持久化，不依赖旧文本是否还在短期窗口；当前轮明确值优先。
- `Memory.MaxHistory` 控制注入模型的最近文本消息数；`Memory.MaxContextTokens` 是系统提示、当前请求和历史消息的严格近似总上限。历史按完整轮次从旧到新淘汰；系统提示与当前请求自身超限时返回 400，不会截断问题或悄悄降级。
- InMemory 使用进程内会话锁，Redis 使用带令牌的分布式锁，PostgreSQL 使用连接级 advisory lock。相同用户的同一会话按顺序执行与删除，不同用户或不同会话可并行。
