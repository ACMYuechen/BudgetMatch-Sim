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
  -> 配置模型时：Eino ReAct LLM Agent 作为编排主入口
       -> eino-ext OpenAI ChatModel 决定工具调用顺序
       -> Eino tools（InferTool 生成）执行本地业务能力
          - search_products
          - select_bundle
          - MCP server 暴露的工具（各自作为一等 Eino 工具）
       -> 工具结果以类型化数据回填结构化商品推荐
       -> 模型未给出套装时回落到确定性选择兜底
  -> 未配置模型时：直接走确定性规则推荐 Agent
  -> 返回 RecommendResp
```

未配置模型时，请求只走确定性规则推荐。配置 OpenAI 后，Eino ReAct LLM Agent 是唯一编排入口，由模型决定工具调用顺序与最终文本总结；结构化商品结果始终由本地工具回填，规则推荐只承担兜底。LLM 链路执行失败时自动降级到规则推荐。

## 代码结构

| 路径 | 说明 |
|------|------|
| `internal/agent/recommend/agent.go` | 确定性规则推荐 Agent，承担无模型兜底 |
| `internal/agent/recommend/service.go` | 推荐编排：primary（LLM）/ fallback（规则）双 Agent，primary 失败自动降级 |
| `internal/agent/recommend/llm/agent.go` | Eino ReAct LLM Agent，LLM 链路的唯一编排入口，实现 `agentcore.Agent` |
| `internal/agent/recommend/llm/chatmodel.go` | 模型工厂，封装 eino-ext 官方 OpenAI `ToolCallingChatModel` |
| `internal/agent/recommend/llm/tools.go` | 用 `utils.InferTool` 从结构体生成业务工具 schema |
| `internal/agent/recommend/llm/session.go` | 单次请求的工具状态（候选缓存、选定套装、工具调用记录） |
| `internal/agent/recommend/llm/decorate.go` | 工具记录 + 错误转 JSON 装饰器 |
| `internal/agent/recommend/llm/mcp.go` | 用 eino-ext `mcp.GetTools` 把 MCP 工具转成一等 Eino 工具 |
| `internal/agent/recommend/llm/prompt.go` | 推荐 Agent system prompt 与用户上下文 prompt |
| `internal/mcp/config.go` | MCP 配置结构 |
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
- `BaseURL` 为空时使用 OpenAI 官方地址；填代理地址时会自动补全 `/v1` 后缀。
- 模型接入直接使用 eino-ext 官方 OpenAI `ToolCallingChatModel`（`github.com/cloudwego/eino-ext/components/model/openai`），不再自写 HTTP 协议，原生支持真流式与重试。

开启后，`Recommend` 主链路构造推荐 prompt，并把以下 Eino tools 暴露给模型：

| 工具名 | 说明 |
|--------|------|
| `search_products` | 根据用户需求、关键词、预算和数量检索候选商品 |
| `select_bundle` | 从候选商品 ID 中选择 MVP 商品套装 |
| MCP server 工具 | MCP 启用时，server 暴露的每个工具各自作为一等 Eino 工具注入（带独立 schema） |

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

- `MCP.Enabled: true` 后，server 暴露的工具会通过 eino-ext `mcp.GetTools` 转成 Eino 工具，直接进入 ReAct 工具集，模型能看到每个工具的名称与参数 schema。
- MCP client 使用 `github.com/mark3labs/mcp-go` 通过 stdio 启动外部进程。
- `Timeout` 单位是毫秒。
- `@modelcontextprotocol/server-everything` 适合本地联调，生产环境应替换成实际需要的 MCP server。

## 本地运行

```bash
cd services/rpc/agent
go run agent.go -f etc/agent.yaml
```

如果需要通过 app 网关访问，还需要启动 `cmd/app`，并确保 app 配置里的 agent RPC client 指向 `agent.rpc`。

## 测试

```bash
go test ./services/rpc/agent/...
go vet ./services/rpc/agent/internal/...
```

## 开发注意

- 业务逻辑写在 `internal/logic/`、`internal/agent/`、`internal/tools/` 下，不要直接编辑 `pb/` 和 `client/` 里的生成代码。
- 不配置模型时，推荐结果来自规则推荐兜底；这不是模型输出。
- 配置 OpenAI 后，模型负责工具调用决策与最终文本总结，结构化商品结果仍由本地工具回填，避免模型编造商品、价格和库存。
- LLM Agent 每次请求新建 session（候选缓存、选定套装、工具记录），不在并发请求间共享状态。
- 工具 schema 由 `utils.InferTool` 从入参结构体推导，新增工具只需定义结构体和 handler，无需手写 JSON Schema。

## 架构说明：Eino ReAct 编排

本次重构遵循「用 Eino，不与 Eino 对着干」的原则，让框架承担它该承担的部分，业务只保留领域能力。

### Eino / eino-ext 负责

- ReAct 多轮模型调用与合法的 `tool_calls` / `role: tool` 消息序列（`flow/agent/react`）。
- OpenAI 模型接入（eino-ext `components/model/openai`，原生支持真流式、重试、Azure）。
- 从结构体推导工具 schema（`components/tool/utils.InferTool`）。
- MCP 工具适配（eino-ext `components/tool/mcp` + `mark3labs/mcp-go`）。

### 业务只保留

- 推荐 prompt、商品检索与套装选择等领域能力（包装成 Eino 工具）。
- gRPC/HTTP 对外接口与确定性推荐兜底。
- 把工具的类型化结果回填到响应。

### 编排边界

- 未配置模型时，`Recommend` 返回确定性规则推荐。
- 配置 OpenAI 后，Eino ReAct LLM Agent 是唯一编排入口，由模型决定工具调用顺序。
- 结构化商品结果由工具以类型化数据回填到 session，不再从消息流反解 JSON。
- 模型未给出套装时回落到确定性选择；LLM 链路整体失败时由 `Service` 降级到规则推荐。
- MCP server 暴露的工具各自作为一等 Eino 工具，带独立 schema，不再藏在 `mcp_call_tool` 元工具后面。
- SSE 接口目前仍是 app 网关侧阶段事件包装，底层 agent-rpc 不是 server-streaming。

### 遗留事项

1. MCP client 复用：当前每次请求启动新的 stdio MCP 子进程，仅在 MCP 启用时发生。MCP 使用频繁时可在 `ServiceContext` 引入可复用 client 或连接池。
2. 真流式 SSE：eino-ext OpenAI 模型已支持真流式，可进一步把 agent-rpc 改为 server-streaming，向网关透出 token / 工具事件。
3. 推荐质量：`planner.parseBudget` 对纯数字仍可能误判（如 `iphone 15` 被识别为预算）；`preferences` 已解析但未参与 selector 打分。属推荐质量优化，与编排架构无关。
