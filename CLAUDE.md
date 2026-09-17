# BudgetMatch-Sim

## 项目概述

BudgetMatch-Sim 是一个面向电商组合决策场景的智能推荐系统原型。项目以 go-zero 微服务架构承载认证、商城、秒杀、支付与推荐 Agent 等核心能力，结合商品价格、库存、预算和用户偏好等多维约束，通过规则引擎、向量检索与 LLM Agent 生成可解释、可落地的购物组合方案。它既是一个高并发电商业务底座，也是一套用于验证 AI Agent 参与真实交易链路决策的工程化实验平台。

## 服务拓扑

下图使用 `make dev` 的默认宿主机访问端口；容器内地址和全容器模式见 [README](README.md#服务架构)。

```
            ┌─────────────┐      ┌─────────────┐
            │  Admin API  │      │   App API   │   REST Gateway (cmd/)
            │   :10001    │      │   :10002    │
            └──────┬──────┘      └──────┬──────┘
                   │                    │
                   └──────────┬─────────┘
                              │ gRPC
       ┌──────────┬───────────┼─────────────┬──────────────┐
       │          │           │             │              │
┌──────┴─────┐ ┌──┴────┐ ┌────┴────┐ ┌──────┴─────┐ ┌──────┴──────┐
│   auth-rpc │ │seckill│ │ mall-rpc│ │  agent-rpc │ │ payment-rpc │       RPC Services (services/rpc/)
│  :10003    │ │:10004 │ │ :10005  │ │  :10006    │ │   :10007    │
└────────────┘ └───────┘ └─────────┘ └────────────┘ └─────────────┘
       │          │           │             │              │
       └──────────┴───────────┴─────────────┴──────────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
     ┌────────┴────────┐  ┌───┴────┐ ┌────────┴─────────┐
     │  PostgreSQL     │  │  etcd  │ │    RocketMQ      │
     │    :15432       │  │ :22379 │ │ :19876 / :10911  │
     │  Redis :6379    │  │        │ │                  │
     └─────────────────┘  └────────┘ └──────────────────┘
```

| 服务 | 端口 | 说明 |
|------|------|------|
| `cmd/admin` | 10001 | 管理后台 REST API |
| `cmd/app` | 10002 | 客户端 REST API |
| `services/rpc/auth` | 10003 (gRPC) | 认证与用户 RPC |
| `services/rpc/seckill` | 10004 (gRPC) | 秒杀活动 RPC |
| `services/rpc/mall` | 10005 (gRPC) | 商城商品与订单 RPC |
| `services/rpc/agent` | 10006 (gRPC) | 推荐 Agent RPC |
| `services/rpc/payment` | 10007 (gRPC) | 支付 RPC（支付宝沙箱当面付） |
| `web-ui` | 5173 / 8080 | Vite 开发入口 / Compose 前端入口 |
| `postgres` | 15432 | 主数据库，容器内为 5432 |
| `redis` | 6379 | 缓存与限流 |
| `etcd` | 22379 / 12379 | `make dev` / Compose 默认宿主机端口，容器内为 2379 |
| `rocketmq` | 19876 / 9876；10911 | NameServer 的 `make dev` / Compose 默认宿主机端口；Broker 端口 |

## 技术栈

- **语言**: Go 1.26.8（以 `go.mod` 为准，工具链维护见 [开发文档](Contributors.md#工具链与依赖版本)）
- **Web 框架**: [go-zero](https://github.com/zeromicro/go-zero)
- **RPC**: gRPC + Protocol Buffers
- **前端**: React + Vite + Ant Design；Node.js 20 与 npm（与当前 CI 保持一致）
- **Agent 框架**: [CloudWeGo Eino](https://github.com/cloudwego/eino) ReAct
- **MCP**: [Model Context Protocol](https://modelcontextprotocol.io/)，通过 `mark3labs/mcp-go` 接入
- **数据库**: PostgreSQL 16（pgvector 镜像，含向量扩展）
- **向量检索**: pgvector + HNSW 余弦索引（agent-rpc 商品语义检索）
- **缓存**: Redis 7（含 agent-rpc 会话记忆，DB 6）
- **部署**: Docker & Docker Compose
- **代码生成**: goctl

## 关键代码路径

### 通用 RPC 服务

| 路径 | 说明 |
|------|------|
| `services/rpc/<service>/proto/<service>.proto` | protobuf 服务定义 |
| `services/rpc/<service>/internal/logic/<service>/` | 业务逻辑（手写） |
| `services/rpc/<service>/internal/server/<service>/` | gRPC server 实现（goctl 生成） |
| `services/rpc/<service>/internal/svc/service_context.go` | 依赖组装 / DI 容器 |
| `services/rpc/<service>/internal/config/config.go` | 配置结构体 |
| `services/rpc/<service>/pb/` | 生成的 protobuf Go 代码（不要编辑） |
| `services/rpc/<service>/client/` | 生成的 RPC 客户端包装（不要编辑） |

### 推荐 Agent（agent-rpc）

| 路径 | 说明 |
|------|------|
| `services/rpc/agent/internal/agent/recommend/service.go` | 双 Agent 编排：`primary`（LLM）/ `fallback`（规则），失败自动降级 |
| `services/rpc/agent/internal/agent/recommend/agent.go` | 确定性规则推荐 Agent（兜底） |
| `services/rpc/agent/internal/agent/recommend/llm/agent.go` | Eino ReAct LLM Agent，LLM 链路唯一编排入口 |
| `services/rpc/agent/internal/agent/recommend/llm/chatmodel.go` | 模型工厂，封装 eino-ext 官方 OpenAI `ToolCallingChatModel` |
| `services/rpc/agent/internal/agent/recommend/llm/tools.go` | 默认注册商品搜索/选择；文件工具默认关闭，启用后按用户与当前保存指令授权 |
| `services/rpc/agent/internal/agent/recommend/llm/mcp.go` | MCP 工具适配，把 MCP server 工具转成一等 Eino 工具 |
| `services/rpc/agent/internal/agent/recommend/llm/prompt.go` | System prompt 与用户上下文 prompt |
| `services/rpc/agent/internal/agent/recommend/llm/session.go` | 单次请求状态管理 |
| `services/rpc/agent/internal/agent/recommend/planner.go` | 意图解析器 |
| `services/rpc/agent/internal/recommend/bundle_selector.go` | 商品套装选择器 |
| `services/rpc/agent/internal/memory/` | 会话记忆（ConversationStore + InMemory/Redis/PostgreSQL/两级缓存实现） |
| `services/rpc/agent/internal/einolog/` | Eino 组件统一日志回调（模型/工具/检索/嵌入观测） |
| `services/rpc/agent/internal/rag/` | RAG：Loader/Indexer/Retriever（Eino 官方组件接口）+ 同步流水线 |
| `services/rpc/agent/model/product_vectors/` | 商品向量表（pgvector，派生数据可安全重建） |
| `services/rpc/agent/internal/tools/product_provider.go` | 商品数据提供者接口 |
| `services/rpc/agent/internal/tools/mall_product_provider.go` | mall-rpc 关键词检索 provider |
| `services/rpc/agent/internal/tools/rag_product_provider.go` | 语义检索 provider（向量优先，关键词回退） |
| `services/rpc/agent/internal/model/config.go` | LLM 模型配置 |
| `services/rpc/agent/internal/model/embedding.go` | Embedding 模型配置 |
| `services/rpc/agent/internal/mcp/config.go` | MCP 客户端配置 |
| `services/rpc/agent/etc/config.yaml` | agent-rpc 服务配置 |

### HTTP Gateway（cmd/app）

| 路径 | 说明 |
|------|------|
| `cmd/app/desc/agent/agent.api` | Agent 相关 HTTP API 定义 |
| `cmd/app/internal/handler/agent/` | HTTP handler（goctl 生成） |
| `cmd/app/internal/logic/agent/` | HTTP 业务逻辑 |

## 错误处理与日志规范

- 统一业务错误定义在 `infra/errors`，`AppError.Error()` 输出 `code:msgId`，HTTP 状态码由错误码前三位决定。
- RPC logic 返回业务错误时直接返回 `infra/errors` 中的错误值，例如 `errors.UserNotFound`、`errors.MallStockNotEnough`、`errors.InvalidToken`。
- API logic 调用 RPC 失败时，先用 `l.Logger.Errorf(...)` 记录上下文，再原样 `return err` / `return nil, err`；不要把 RPC 返回的业务错误包装成 `errors.Internal`、`errors.Database` 等本地错误，否则客户端会丢失真实业务错误码。
- API logic 的本地校验错误仍然直接返回本地 `infra/errors`，例如未登录、参数非法、RPC 响应对象为空等不来自 RPC `err` 的分支。
- logic 层所有 error 返回点都必须至少打印一条 `logx` 日志，优先使用 go-zero 生成的 `l.Logger.Errorf(...)`，日志内容要包含操作语义和原始错误。

## 开发工作流

### 1. 初始化环境

```bash
cp .env.example .env
# 编辑 .env 填入真实密钥
```

### 2. 一键启动本地开发环境

```bash
make dev
```

启动顺序：基础设施（postgres / redis / rocketmq / etcd）→ auth-rpc → seckill-rpc → mall-rpc → agent-rpc → payment-rpc → app → admin。

该命令不启动前端；另开终端在 `web-ui` 下运行 `npm ci`、`npm run dev`。用户端入口为 `http://localhost:5173`，管理端为 `/admin`。开发代理区分 `/api/admin` → `10001` 与其余 `/api` → `10002`。前端实现进度、接口边界与模拟测试见 [前端路线图](docs/frontend-roadmap.md)。

### 3. 生成代码

```bash
make api-all
```

会重新生成所有 API / RPC 的 goctl 代码。生成后检查 `pb/`、`client/`、`types.go`、`routes.go` 等文件是否符合预期。

### 4. 测试

```bash
# Go 测试（需配置独立测试环境，见 docs/ci.md）
make test

# 冒烟测试（验证端口与健康检查）
make smoke-test
```

### 5. 停止服务

```bash
make dev-stop
```

## 配置说明

### 环境变量

必要变量见 `.env.example`。本地开发时，`scripts/dev.sh` 会自动加载 `.env`。

### agent-rpc 模型配置

`services/rpc/agent/etc/config.yaml`：

```yaml
Model:
  Provider: "${LLM_PROVIDER}"      # 如 openai（OpenAI 兼容接口均可）
  Model: "${LLM_MODEL}"            # 如 gpt-4.1-mini / deepseek-chat
  BaseURL: "${LLM_BASE_URL}"       # 如 https://api.openai.com/v1 或 https://api.deepseek.com/v1
  APIKey: "${LLM_API_KEY}"         # 模型服务 API 密钥
```

- `Provider` 为空（或未设置）时，agent-rpc 走确定性规则推荐，不请求外部模型。
- Provider 为 `openai` 时使用 eino-ext 官方 OpenAI ChatModel，兼容 DeepSeek、Azure 等 OpenAI 兼容接口。
- `BaseURL` 支持以 `/v1` 结尾的基地址，也支持 `/v1/chat/completions` 等完整路径（会自动识别 `/v1` 前缀，避免重复拼接）。

### agent-rpc 依赖降级矩阵

agent-rpc 的全部外部依赖均可选，任意缺失都能启动：

| 配置缺失 | 行为 |
|------|------|
| `Model`（LLM） | 推荐走确定性规则，不请求外部模型 |
| `Embedding` | RAG 关闭，商品检索走关键词模式 |
| `MallRpc` | 商品数据用内存 mock（配置了 mall 则绝不混用 mock） |
| `Database` | RAG 关闭；会话记忆使用 Redis，Redis 也未配置时退回进程内实现 |
| `CacheRedis` | PostgreSQL 可用时只关闭一级缓存；PostgreSQL 未配置时退回进程内实现 |

- **Embedding 与 LLM 是两套独立配置**（`EMBEDDING_*` 环境变量）：DeepSeek 无 embeddings 接口，RAG 需要 OpenAI / DashScope 等兼容服务。
- **商品向量表是派生数据**：`product_vectors` 表可随时删除，同步器会自动回填；换 embedding 模型或维度会触发删表重建 + 全量重嵌入（有 token 成本）。
- **多轮对话**：`conversation_id` 标识会话，`turn_id` 保证请求幂等；PostgreSQL 可长期保存 Conversation/Turn 和结构化状态，Redis 仅作两级缓存或短期降级，窗口/TTL 见 `Memory` 配置。

### MCP 配置

```yaml
MCP:
  Enabled: false
  Command: ""       # 已安装、经审计的服务端可执行文件绝对路径
  Args: []
  AllowedTools: []   # 空列表不启动；只允许精确名称且 readOnlyHint=true 的工具
  Timeout: 5000
```

启用后只注入白名单工具，模型名称加 `mcp_` 前缀。子进程不继承服务密钥，使用私有临时目录、超时和进程组清理；不是文件系统/网络沙箱，启用前必须审计服务端。文件工具另由 `FileTools.Enabled` 控制，写入还需 `AllowWrite` 与本轮首行 `/save <相对路径>`，只创建当前用户文件、不覆盖。具体配置与限制见 [Agent 开发文档](docs/agent.md#28-文件工具与-mcp-权限)。

## 常见任务

### 如何调试 agent-rpc

离线规则评测可先执行 `go run ./services/rpc/agent/cmd/eval -format markdown`：不加载 `.env`，不连接模型/数据库。64 条合成用例尚待独立人工复核；M2.2 修复两条预算变更反例，当前门禁退出 0，但可满足任务仍只有 25/40 成功。`-compare services/rpc/agent/testdata/eval/baseline.v1/report.json` 保留同数据版本对比；`-suite scripted` 另跑 16 个 Fake Model + 真实 ReAct 容错场景，不调用外部模型。指标、划分和未运行项见 [Agent M2 文档](docs/agent.md#7-m2评测集与可重复基线)。不要改写原始报告/答案或移动保留集掩盖失败；已知 holdout 只作为回归集，不再当作盲测证据。

M2.3a 可用 `go run ./services/rpc/agent/cmd/eval-review -out /tmp/agent-review-001` 生成独立复核材料，再用 `-check <review.json>` 校验记录。归档 `review.v1` 只保留全 pending 模板，实际人工记录另存；不得代填人工身份或将 records_complete 视为人工验收。M3/M4 目标目前为待确认草案，真实模型/Embedding 调用与费用默认均为 0，未获授权不得启动。

```bash
# 从仓库根目录运行，并提前配置服务所需环境变量
go run -C services/rpc/agent . -f etc/config.yaml
# 或查看日志
tail -f logs/agent-rpc.log
```

### 如何添加新的 Eino 工具

1. 在 `services/rpc/agent/internal/agent/recommend/llm/tools.go` 中定义入参结构体（使用 `jsonschema` tag）。
2. 实现 handler 函数。
3. 使用 `utils.InferTool` 从结构体推导 schema 并注册到工具列表。
4. 无需手写 JSON Schema。

### 如何切换 LLM 模型

修改 `.env` 中的 `LLM_PROVIDER`、`LLM_MODEL`、`LLM_BASE_URL`、`LLM_API_KEY`，然后重启 `agent-rpc`。

### 如何测试推荐接口

```bash
# 同步推荐（响应中的 conversation_id 用于发起下一轮）
curl -X POST http://localhost:10002/api/agent/recommend \
	-H "Authorization: Bearer <登录返回的token>" \
  -H "Content-Type: application/json" \
	-d '{"query":"预算5000买手机","budget_cents":500000,"max_items":3,"turn_id":"<本轮UUID>"}'

# 多轮对话：携带上一轮返回的 conversation_id
curl -X POST http://localhost:10002/api/agent/recommend \
	-H "Authorization: Bearer <登录返回的token>" \
  -H "Content-Type: application/json" \
	-d '{"query":"预算加到8000，换个屏幕好点的","conversation_id":"<上一轮返回的ID>","turn_id":"<新的本轮UUID>"}'

# SSE 阶段事件流
curl -X POST http://localhost:10002/api/agent/recommend/stream \
	-H "Authorization: Bearer <登录返回的token>" \
  -H "Content-Type: application/json" \
	-d '{"query":"预算5000买手机","turn_id":"<本轮UUID>"}'
```

## 注意事项

- **Gateway 层不直连数据库**：`cmd/admin` 和 `cmd/app` 的数据操作必须通过对应 RPC 服务完成。
- **auth-rpc 先启动**：本地 `make dev` 中 auth-rpc 最先启动，负责自动建表（agent-rpc 的向量表由自己建）。
- **agent-rpc 商品数据来自 mall-rpc**：语义检索（RAG）优先，关键词回退；未配置 `MallRpc` 时才使用 `mock_product_provider.go`（详见"依赖降级矩阵"）。RAG 演示前需先给 mall 造商品数据。
- **agent-rpc 无模型时自动降级**：未配置 LLM 时，走确定性规则推荐；配置 LLM 后，失败也会降级到规则推荐。
- **RAG 首轮同步有延迟**：服务启动后商品向量在后台异步索引，完成前检索回退关键词模式，日志出现 `rag sync completed` 即就绪。
- **SSE 是阶段事件流**：`POST /api/agent/recommend/stream` 目前是网关侧包装的阶段事件流，底层 agent-rpc 仍是 unary RPC，不是 token 级或工具调用级真实流式。
- **不要编辑生成代码**：`pb/`、`client/`、`types.go`、`routes.go` 等由 goctl 生成，修改会被 `make api-all` 覆盖。
- **MCP 并发治理待补**：启用且白名单非空时，每请求启动独立 stdio 子进程；已有超时与回收，尚无进程并发配额。是否池化应依据耗时证据，并防止跨用户状态串用。

## 分层规范

相关开发规范见：

- [开发与 Git 提交规范](Contributors.md)
- [CI 说明](docs/ci.md)
- [前端进度与验证](docs/frontend-roadmap.md)
- [错误库规范](infra/errors/README.md)

## 接口文档

- Admin API 定义：[cmd/admin/desc/admin.api](cmd/admin/desc/admin.api)
- App API 定义：[cmd/app/desc/app.api](cmd/app/desc/app.api)

`make api-all` 会生成 `docs/admin-api.json`、`docs/app-api.json`，这些 JSON 被 Git 忽略，干净检出中不保证存在。命令同时重新生成 API/RPC 代码，执行后需检查差异。
