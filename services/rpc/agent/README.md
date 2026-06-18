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
