# agent-rpc

agent-rpc 是推荐 Agent 的 gRPC 服务，负责理解用户的商品套装需求，检索候选商品，并生成 MVP 商品组合推荐。当前服务支持本地规则兜底，以及基于 CloudWeGo Eino ReAct 的 LLM function calling 主流程。

## 服务信息

| 项 | 值 |
|----|-----|
| 服务名 | `agent.rpc` |
| 监听地址 | `0.0.0.0:10006` |
| 协议 | gRPC + Protocol Buffers |
| 对外入口 | `cmd/app` HTTP 网关 |
| 当前 Agent | `recommend_agent` |
| Agent 编排 | CloudWeGo Eino ReAct |

## 调用入口

### RPC

| Service | Method | 说明 |
|---------|--------|------|
| `RecommendService` | `Recommend` | 根据用户需求生成商品套装推荐 |

### HTTP Gateway

网关侧接口在 `cmd/app` 中注册，网关只做请求解析和 RPC 转发，不承载 Agent 业务逻辑。

| Method | Path | 说明 |
|--------|------|------|
| POST | `/api/agent/recommend` | 普通推荐接口 |
| POST | `/api/agent/recommend/stream` | SSE 阶段事件接口 |

当前 SSE 接口是阶段事件流，会输出 `request.accepted`、`rpc.started`、`recommendation.final`、`done` 等事件；底层 agent-rpc 仍是 unary RPC，还不是 token 级或工具调用级真实流式。

## 当前执行链路

```text
HTTP POST /api/agent/recommend
  -> cmd/app agent logic
  -> gRPC RecommendService.Recommend
  -> recommend_agent 生成本地规则推荐草稿
  -> 如果配置模型，则进入 Eino ReAct runtime
  -> Eino ToolCallingChatModel 决定是否调用工具
  -> Eino tools 调用本地 toolkit executor
     - search_products
     - select_bundle
     - mcp_call_tool
  -> 工具结果回填结构化商品推荐
  -> 返回 RecommendResp
```

默认不配置模型时，请求只走规则推荐兜底。配置 OpenAI 后，请求会在规则草稿基础上进入 Eino ReAct function calling，由模型决定工具调用顺序和最终文本总结。

## 代码结构

| 路径 | 说明 |
|------|------|
| `internal/agent/recommend/agent.go` | 本地规则推荐器，负责无模型兜底和生成初始草稿 |
| `internal/agent/recommend/eino/runner.go` | Eino ReAct runtime，负责模型调用、工具循环和工具结果收集 |
| `internal/agent/recommend/eino/openai_model.go` | OpenAI-compatible Chat Completions 适配器，实现 Eino `ToolCallingChatModel` |
| `internal/agent/recommend/eino/tools.go` | 将本地工具封装为 Eino `tool.BaseTool` |
| `internal/agent/recommend/eino/prompt.go` | 推荐 Agent system prompt 和用户上下文 prompt |
| `internal/agent/recommend/toolkit/` | 本地工具执行器，包含商品检索、套装选择、MCP 调用 |
| `internal/mcp/` | stdio MCP client 与 MCP 集成测试 |
| `internal/modelconfig/` | 模型配置结构 |

## 配置说明

配置文件位于 `services/rpc/agent/etc/agent.yaml`。

### 默认本地模式

```yaml
Model:
  Provider: ""
  Model: ""
  BaseURL: ""
  APIKey: ""

MCP:
  Enabled: false
```

`Provider` 为空时不创建 Eino LLM runtime，也不会请求外部模型。服务仍可正常运行，并返回本地规则推荐结果。

### 开启 OpenAI Function Calling

```yaml
Model:
  Provider: openai
  Model: gpt-4.1-mini
  BaseURL: https://api.openai.com
  APIKey: ${OPENAI_API_KEY}
```

注意：

- `Provider: openai` 时必须配置 `APIKey`，否则 agent-rpc 会启动失败。
- `Model` 为空时默认使用 `gpt-4.1-mini`。
- `BaseURL` 为空时默认使用 `https://api.openai.com`，也可以改成兼容 OpenAI Chat Completions 的代理地址。
- 当前 OpenAI 适配器使用 Chat Completions `tools/tool_calls` 协议，并通过 Eino `ToolCallingChatModel` 接入 ReAct。

开启后，`Recommend` 主链路会构造推荐 prompt，并把以下 Eino tools 暴露给模型：

| 工具名 | 说明 |
|--------|------|
| `search_products` | 根据用户需求、关键词、预算和数量检索候选商品 |
| `select_bundle` | 从候选商品 ID 中选择 MVP 商品套装 |
| `mcp_call_tool` | 调用已启用 MCP server 暴露的工具 |

## MCP 配置

默认配置预留了一个免费第三方 MCP 示例 server：

```yaml
MCP:
  Enabled: false
  Command: npx
  Args:
    - -y
    - @modelcontextprotocol/server-everything
    - stdio
  Timeout: 5000
```

开启 MCP：

```yaml
MCP:
  Enabled: true
  Command: npx
  Args:
    - -y
    - @modelcontextprotocol/server-everything
    - stdio
  Timeout: 5000
```

说明：

- `MCP.Enabled: true` 后，Eino function calling 中的 `mcp_call_tool` 才能调用 MCP 工具。
- 当前 MCP client 通过 stdio 启动外部进程。
- `Timeout` 单位是毫秒。
- `@modelcontextprotocol/server-everything` 适合本地联调，生产环境应替换成实际需要的 MCP server。

## 本地运行

```bash
cd services/rpc/agent
go run agent.go -f etc/agent.yaml
```

如果需要通过 app 网关访问，还需要启动 `cmd/app`，并确保 app 配置里的 agent RPC client 指向 `agent.rpc`。

## MCP 集成测试

默认单测不会请求第三方 MCP server。需要验证 `@modelcontextprotocol/server-everything` 时手动开启：

```bash
AGENT_MCP_INTEGRATION=1 go test ./services/rpc/agent/internal/mcp -run TestIntegrationEverythingServer
```

可通过环境变量覆盖 MCP 命令：

```bash
AGENT_MCP_INTEGRATION=1 \
AGENT_MCP_COMMAND=npx \
AGENT_MCP_ARGS="-y @modelcontextprotocol/server-everything stdio" \
go test ./services/rpc/agent/internal/mcp -run TestIntegrationEverythingServer
```

## 测试

```bash
go test ./services/rpc/agent/...
go vet ./services/rpc/agent/internal/...
```

## 开发注意

- 业务逻辑写在 `internal/logic/`、`internal/agent/`、`internal/tools/` 下，不要直接编辑 `pb/` 和 `client/` 里的生成代码。
- 不配置模型时，推荐结果来自规则推荐兜底；这不是模型输出。
- 配置 OpenAI 后，模型负责 function calling 决策和最终文本总结，结构化商品结果仍由本地工具结果回填，避免模型编造商品、价格和库存。
- Eino runtime 每次请求都会创建新的 toolkit executor 和 candidate store，避免并发请求之间共享候选商品状态。
- MCP 工具参数是动态 JSON，`mcp_call_tool` 的 `arguments` 保持对象类型，便于适配不同第三方 MCP 工具。

## 迁移说明：Eino ReAct

这次迁移后，旧的自写 `flow`、`prompt`、`llm` 包已经删除，function calling 主流程改由 Eino ReAct 承接。

### 迁移目标

迁移前，项目里有自写 function calling loop、自写消息结构、自写 LLM client 抽象。它们可以跑通，但后续要扩多 agent、接更多工具、做中间事件或流式输出时，维护成本会越来越高。

迁移后，Eino 负责：

- 管理 ReAct 多轮模型调用。
- 将 tools schema 注入模型。
- 维护合法的 assistant `tool_calls` 与 `role: tool` 消息序列。
- 执行工具节点并收集中间消息。
- 为后续多 agent 编排留出框架空间。

项目自己保留：

- 推荐业务 prompt。
- OpenAI-compatible Eino ChatModel 适配器。
- 商品检索、套装选择、MCP 调用这些本地业务工具。
- gRPC/HTTP 对外接口和确定性推荐兜底。

### 当前边界

- 无模型配置时，`Recommend` 返回确定性规则推荐结果。
- 配置 OpenAI 后，`Recommend` 会进入 Eino ReAct function calling。
- 模型只负责工具调用决策和最终文本总结。
- 商品明细、价格、库存等结构化结果由本地工具结果回填。
- `mcp_call_tool` 是动态 MCP 工具桥接入口，不绑定某个固定第三方工具。
- SSE 接口目前仍是 app 网关侧阶段事件包装，底层 agent-rpc 不是 server-streaming。

### 已解决的问题

| 问题 | 状态 |
|------|------|
| function calling 链路构造了但主请求不使用 | 已修 |
| OpenAI provider 未实现 | 已修 |
| `openai` 无 API key 时静默退回 noop | 已修 |
| tool calling 消息序列需要手动维护 | 已由 Eino ReAct 接管 |
| toolkit executor 跨请求共享 candidate store | 已修，每次请求新建 executor |
| 普通推荐链路同步 MCP probe 导致阻塞 | 已修，确定性推荐不再触发 MCP probe |

### 遗留事项

1. MCP client 复用

当前 `mcp_call_tool` 每次调用仍会启动新的 stdio MCP 子进程。现在只有模型显式调用 MCP 时才会发生；如果后续 MCP 使用频繁，建议在 `ServiceContext` 中引入可复用 client 或连接池，并处理重连。

2. `mcp.Probe` 的定位

`mcp.Probe` 当前只被 MCP 自身测试使用。后续可以接到健康检查、启动自检或调试接口；如果没有使用场景，也可以删除，避免误导。

3. 更稳的工具兜底

当前 prompt 要求模型先调用 `search_products` 再调用 `select_bundle`。如果模型直接调用 `select_bundle`，会因为 candidate store 为空而返回工具错误。后续可以在 `select_bundle` 候选为空时做一次自动 search 兜底。

4. 真流式 SSE

现在 SSE 只输出固定阶段事件。若要展示模型 token、工具调用开始、工具结果返回等过程，需要把 agent-rpc 改为 server-streaming，或让 Eino runtime 暴露中间事件给 app 网关转发。

5. 推荐质量增强

`planner.parseBudget` 对纯数字仍可能误判，例如 `iphone 15` 被识别为预算；`preferences` 已解析但还没有参与 selector 打分。这两个属于推荐质量优化，不影响当前 Eino 架构。
