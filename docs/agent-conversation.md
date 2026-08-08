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

Memory.MaxHistory 控制保留的消息窗口；窗口满后会从最早消息开始截断，不会无限增长。配置了 Redis 时，Memory.TTL 是滑动过期时间，每次写入都会刷新；未配置 Redis 时自动降级为进程内存记忆，适用于本地开发和单实例运行。
