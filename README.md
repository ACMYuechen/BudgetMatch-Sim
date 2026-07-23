# BudgetMatch-Sim

BudgetMatch-Sim 是一个面向电商组合决策场景的智能推荐系统原型。项目以 go-zero 微服务架构承载认证、商城、秒杀、支付与推荐 Agent 等核心能力，结合商品价格、库存、预算和用户偏好等多维约束，通过规则引擎、向量检索与 LLM Agent 生成可解释、可落地的购物组合方案。它既是一个高并发电商业务底座，也是一套用于验证 AI Agent 参与真实交易链路决策的工程化实验平台。

## 技术栈

- **语言**: Go 1.22+
- **Web 框架**: [go-zero](https://github.com/zeromicro/go-zero)
- **RPC**: gRPC + Protocol Buffers
- **Agent 框架**: [CloudWeGo Eino](https://github.com/cloudwego/eino) ReAct
- **MCP**: [Model Context Protocol](https://modelcontextprotocol.io/)（通过 `mark3labs/mcp-go` 接入）
- **数据库**: PostgreSQL 16
- **缓存**: Redis 7
- **服务注册**: etcd
- **消息队列**: RocketMQ
- **部署**: Docker & Docker Compose
- **代码生成**: goctl

## 服务架构

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
     │    :5432        │  │ :12379 │ │ :9876 / :10911   │
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
| `postgres` | 5432 | 主数据库 |
| `redis` | 6379 | 缓存与限流 |
| `etcd` | 12379 | 服务注册与动态配置 |
| `rocketmq` | 9876 / 10911 | 消息队列 |

## 快速开始

### 1. 前置依赖

- Go 1.22+
- Docker & Docker Compose
- [goctl](https://go-zero.dev/docs/tasks/installation/goctl)（生成代码时需要）

### 2. 配置环境变量

```bash
cp .env.example .env
```

编辑 `.env` 填入你的真实密钥。必要变量：

| 变量 | 说明 |
|------|------|
| `JWT_SECRET` | JWT 签名密钥，建议 ≥ 32 位随机字符串 |
| `EMAIL_FROM` | 发件邮箱（如 QQ 邮箱） |
| `EMAIL_PASSWORD` | 邮箱 SMTP 授权码 |
| `LLM_PROVIDER` | LLM 服务商（如 `openai`），留空则走本地规则推荐 |
| `LLM_MODEL` | 模型名称（如 `gpt-4.1-mini` / `deepseek-chat`） |
| `LLM_BASE_URL` | 模型 API 地址（如 `https://api.openai.com/v1`） |
| `LLM_API_KEY` | 模型 API 密钥 |

> 完整密钥说明见 [docs/SECRETS.md](docs/SECRETS.md)。`.env` 已加入 `.gitignore`，不会提交到仓库。

### 3. 一键启动

```bash
make dev
```

该命令会：
1. 加载 `.env` 环境变量
2. 启动 PostgreSQL、Redis、etcd、RocketMQ 等基础设施
3. 启动 auth-rpc、seckill-rpc、mall-rpc、agent-rpc、payment-rpc、app、admin 七个服务

### 4. 验证

```bash
make smoke-test
```

### 5. 停止

```bash
make dev-stop
```

## 常用命令

```bash
# 查看帮助
make help

# 生成所有 API/RPC 代码
make api-all

# 运行单元测试
make test

# 查看服务日志
tail -f logs/auth-rpc.log
tail -f logs/seckill-rpc.log
tail -f logs/mall-rpc.log
tail -f logs/agent-rpc.log
tail -f logs/payment-rpc.log
tail -f logs/app.log
tail -f logs/admin.log

# 测试推荐接口
curl -X POST http://localhost:10002/api/agent/recommend \
  -H "Content-Type: application/json" \
  -d '{"query":"预算5000买手机","budget_cents":500000,"max_items":3}'

# Docker 全量部署
make docker-up
make docker-down
```

## 目录结构

```
.
├── cmd/                # REST API Gateway 层
│   ├── admin/          # 管理后台
│   └── app/            # 客户端 API
├── services/           # gRPC 业务服务层
│   └── rpc/
│       ├── auth/       # 认证与用户服务
│       ├── seckill/    # 秒杀服务
│       ├── mall/       # 商城商品与订单服务
│       ├── agent/      # 推荐 Agent 服务
│       └── payment/    # 支付服务（支付宝沙箱当面付）
├── infra/              # 基础设施封装（数据库、Redis、JWT、OSS、限流等）
├── docs/               # 文档与生成的 Swagger
├── scripts/            # 开发脚本
├── tpls/               # goctl 模板
├── Makefile            # 常用命令
├── docker-compose.yml  # 基础设施编排
└── Dockerfile          # 服务构建镜像
```

## 关键路径

| 路径 | 说明 |
|------|------|
| `cmd/<app>/desc/**/*.api` | REST API 定义 |
| `cmd/<app>/internal/logic/` | API 层业务逻辑（手写） |
| `cmd/<app>/internal/handler/` | HTTP handler（goctl 生成） |
| `services/rpc/<service>/proto/<service>.proto` | RPC protobuf 定义 |
| `services/rpc/<service>/internal/logic/` | RPC 层业务逻辑（手写） |
| `services/rpc/<service>/pb/` | 生成的 protobuf Go 代码（不要编辑） |
| `services/rpc/<service>/client/` | 生成的 RPC 客户端包装（不要编辑） |
| `infra/errors` | 统一业务错误码、文案和 HTTP 状态映射 |
| `infra/interceptor` | gRPC 认证与 token 透传拦截器 |

## Agent 能力

- LLM 链路使用 Eino ReAct Agent，入口在 `services/rpc/agent/internal/agent/recommend/llm/agent.go`。
- 内置 Eino 工具在 `services/rpc/agent/internal/agent/recommend/llm/tools.go`，包括 `search_products`、`select_bundle`、`read_file`、`write_file`。
- 支持 MCP 工具注入，配置在 `services/rpc/agent/etc/config.yaml` 的 `MCP` 段，适配代码在 `services/rpc/agent/internal/agent/recommend/llm/mcp.go`。
- 支持多轮对话，`conversation_id` 首轮可为空，由服务端生成并回传；Redis 可用时会话记忆存 Redis DB 6，否则退回进程内实现。

## 错误处理与日志规范

- 统一业务错误定义在 `infra/errors`，`AppError.Error()` 输出 `code:msgId`，HTTP 状态码由错误码前三位决定。
- RPC logic 返回业务错误时直接返回 `infra/errors` 中的错误值，例如 `errors.UserNotFound`、`errors.MallStockNotEnough`、`errors.InvalidToken`。
- API logic 调用 RPC 失败时，先用 `l.Logger.Errorf(...)` 记录上下文，再原样 `return err` / `return nil, err`；不要把 RPC 返回的业务错误包装成 `errors.Internal`、`errors.Database` 等本地错误，否则客户端会丢失真实业务错误码。
- API logic 的本地校验错误仍然直接返回本地 `infra/errors`，例如未登录、参数非法、RPC 响应对象为空等不来自 RPC `err` 的分支。
- logic 层所有 error 返回点都必须至少打印一条 `logx` 日志，优先使用 go-zero 生成的 `l.Logger.Errorf(...)`，日志内容要包含操作语义和原始错误。

各层详细规范见：
- [cmd/README.md](cmd/README.md)
- [services/README.md](services/README.md)
- [infra/README.md](infra/README.md)

## 接口文档

go-zero API 定义生成 Swagger 后存放于 `docs/`：

- Admin API: [docs/admin-api.json](docs/admin-api.json)
- App API: [docs/app-api.json](docs/app-api.json)

可通过 Swagger UI 或导入 Postman 查看。

## 开发注意

- 服务本地启动时会自动建表（`AutoMigrate: true`）。
- `cmd/admin` 和 `cmd/app` 不直连数据库，数据操作通过对应 RPC 服务完成。
- `agent-rpc` 不配置 LLM 时自动走确定性规则推荐；配置后由 Eino ReAct Agent 编排 LLM 工具调用，失败时自动降级到规则推荐。
- 不要编辑 `pb/`、`client/`、`types.go`、`routes.go` 等生成代码，重新执行 `make api-all` 会覆盖这些文件。
