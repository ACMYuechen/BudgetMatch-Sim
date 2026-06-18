# agent-rpc

agent-rpc 是推荐 Agent 的 gRPC 服务，负责理解用户的商品套装需求，检索候选商品，并生成 MVP 商品组合推荐。当前服务同时支持本地规则兜底和 LLM function calling 主流程。

## 服务信息

| 项 | 值 |
|----|-----|
| 服务名 | `agent.rpc` |
| 监听地址 | `0.0.0.0:10006` |
| 协议 | gRPC + Protocol Buffers |
| 对外入口 | `cmd/app` HTTP 网关 |
| 当前 Agent | `recommend_agent` |

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
  -> recommend_agent 生成规则推荐草稿
  -> 如果启用 LLM，则进入 prompt + function calling flow
  -> 本地工具执行器 search_products / select_bundle / mcp_call_tool
  -> 返回 RecommendResp
```

默认不配置模型时，请求只走规则推荐兜底。配置 OpenAI 后，请求会在规则草稿基础上进入 function calling flow，由模型决定是否调用本地工具和 MCP 工具。

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

`Provider` 为空时会使用 noop LLM client，不会请求外部模型。服务仍可正常运行，并使用规则推荐结果返回。

### 开启 OpenAI function calling

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
- 当前实现使用 Chat Completions `tools/tool_calls` 协议。

开启后，`Recommend` 主链路会构造推荐 prompt，并把以下工具暴露给模型：

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

- `MCP.Enabled: true` 后，function calling flow 中的 `mcp_call_tool` 才能调用 MCP 工具。
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
```

## 开发注意

- 业务逻辑写在 `internal/logic/`、`internal/agent/`、`internal/tools/` 下，不要直接编辑 `pb/` 和 `client/` 里的生成代码。
- 不配置模型时，推荐结果来自规则推荐兜底；这不是模型输出。
- 配置 OpenAI 后，模型只负责 function calling 和最终文本总结，结构化商品结果仍由本地工具结果回填，避免模型编造商品、价格和库存。
- MCP 工具参数是动态 JSON，`mcp_call_tool` 的 schema 不强制 strict，便于适配不同第三方 MCP 工具。

## PR 说明：推荐 Agent function calling 转正

### 背景

本轮改动前，agent-rpc 已经有 prompt、LLM client、function calling runner、toolkit executor、MCP client 等基础模块，但主请求链路仍然只走确定性规则推荐。也就是说，OpenAI function calling 与本地工具执行器虽然被编译进服务，却没有真正承接 `Recommend` 请求。

这轮 PR 的目标是把推荐 Agent 从“规则推荐 + 预留骨架”推进到“规则草稿 + LLM function calling 精修”的可用架构，同时保留无模型配置时的本地可运行能力。

### 提交范围

| Commit | 说明 |
|--------|------|
| `feat(agent): 转正推荐 Agent 的 function calling 主链路` | 将 `RecommendFlow` 接入 gRPC `Recommend` 主链路，修正 tool calling 消息历史，并让每次 flow run 创建独立工具执行器 |
| `feat(agent): 转正推荐 Agent 的 function calling 主链路` | 新增 OpenAI-compatible Chat Completions client，实现 `/v1/chat/completions` 的 `tools/tool_calls` 调用与解析 |
| `docs(agent): 补充 LLM function calling 与 MCP 配置说明` | 新增 agent-rpc README，说明 LLM、MCP、HTTP/SSE 入口与本地运行方式 |
| `fix(agent): 避免推荐链路同步阻塞 MCP 探测` | 从确定性推荐路径移除同步 MCP probe，避免普通推荐请求被 MCP 子进程启动和探活阻塞 |

### 主要改动

1. `Recommend` 主链路接入 function calling

`services/rpc/agent/internal/logic/recommendservice/recommend_logic.go` 现在会先生成确定性推荐草稿；当 `RecommendFlowEnabled()` 为真时，再进入 prompt + function calling flow。这样默认本地模式仍然稳定，配置 OpenAI 后才启用真实模型链路。

2. 实现 OpenAI-compatible LLM client

`services/rpc/agent/internal/llm/openai.go` 新增 Chat Completions client，支持：

- 请求 `/v1/chat/completions`
- 发送 `tools`
- 解析 assistant `tool_calls`
- 将 function name 和 arguments 转为内部 `llm.ToolCall`

`Provider: openai` 但未配置 `APIKey` 时会直接报错，不再静默退回 noop，避免误以为已经在调用模型。

3. 修正 function calling 消息协议

`flow.Runner` 现在会先把带 `tool_calls` 的 assistant 消息写入历史，再追加对应的 `role: tool` 消息，并把 `tool_call_id` 放在消息顶层字段。这样后续接真实 OpenAI API 时，消息序列是合法的。

4. 隔离每次请求的工具执行器状态

`RecommendFlow` 改为使用 executor factory。每次 `Run` 都会创建新的 toolkit executor 和 candidate store，避免并发请求之间共享候选商品状态。

5. 保留 MCP 工具能力，但移除普通推荐同步探活

确定性推荐路径不再调用 `mcp.Probe`。MCP 仍可通过 function calling flow 中的 `mcp_call_tool` 调用，只是在模型明确使用 MCP 工具时才会启动 MCP client。

### 已解决的问题

| 问题 | 状态 |
|------|------|
| function calling 链路构造了但主请求不使用 | 已修 |
| OpenAI provider 未实现 | 已修 |
| `openai` 无 API key 时静默退回 noop | 已修 |
| tool calling 消息序列缺少 assistant `tool_calls` | 已修 |
| `tool_call_id` 未放在 tool 消息顶层 | 已修 |
| toolkit executor 跨请求共享 candidate store | 已修 |
| 普通推荐链路同步 MCP probe 导致阻塞 | 已修 |

### 当前架构边界

- 无模型配置时，`Recommend` 返回确定性规则推荐结果。
- 配置 OpenAI 后，`Recommend` 会进入 function calling flow。
- 模型负责工具调用决策和最终文本总结。
- 商品明细、价格、库存等结构化结果仍由本地工具结果回填，避免模型编造。
- `mcp_call_tool` 是动态 MCP 工具桥接入口，不绑定某个固定第三方工具。
- SSE 接口目前仍是 app 网关侧阶段事件包装，底层 agent-rpc 不是 server-streaming。

### 验证结果

已验证：

```bash
go test ./services/rpc/agent/...
go test ./...
```

此前还针对 agent 内部改动跑过：

```bash
go vet ./services/rpc/agent/internal/...
```

相关测试覆盖：

- OpenAI client 能解析 `tool_calls`
- runner 会追加 assistant `tool_calls` 和 tool 消息
- runner 每次 run 使用新的 executor
- prompt 工具 schema 与 MCP 动态参数约束
- 普通推荐不会触发 MCP probe

### 遗留事项

1. MCP client 复用

当前 `mcp_call_tool` 每次调用仍会启动新的 stdio MCP 子进程。现在只有模型显式调用 MCP 时才会发生，频率已经降低；如果后续 MCP 使用频繁，建议在 `ServiceContext` 中引入可复用 client 或连接池，并处理重连。

2. `mcp.Probe` 的定位

`mcp.Probe` 当前只被 MCP 自身测试使用。后续可以接到健康检查、启动自检或调试接口；如果没有使用场景，也可以删除，避免误导。

3. 更稳的工具兜底

当前 prompt 要求模型先调用 `search_products` 再调用 `select_bundle`。如果模型直接调用 `select_bundle`，会因为 candidate store 为空而返回工具错误。后续可以在 `select_bundle` 候选为空时做一次自动 search 兜底。

4. 真流式 SSE

现在 SSE 只输出固定阶段事件。若要展示模型 token、工具调用开始、工具结果返回等过程，需要把 agent-rpc 改为 server-streaming，或让 flow 暴露中间事件给 app 网关转发。

5. 推荐质量增强

`planner.parseBudget` 对纯数字仍可能误判，例如 `iphone 15` 被识别为预算；`preferences` 已解析但还没有参与 selector 打分。这两个属于推荐质量优化，不影响当前 function calling 架构。
