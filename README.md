# BudgetMatch-Sim

以历史价格时序数据推演虚拟电商市场，在严格预算约束下，通过自研规则与策略引擎，输出最优个性化购物选配方案。

## 技术栈

- **语言**: Go 1.22+
- **Web 框架**: [go-zero](https://github.com/zeromicro/go-zero)
- **RPC**: gRPC + Protocol Buffers
- **数据库**: PostgreSQL 16
- **缓存**: Redis 7
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
                     ┌────────┴────────┐
                     │  PostgreSQL     │
                     │    :5432        │
                     │  Redis :6379    │
                     └─────────────────┘
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

> 完整密钥说明见 [docs/SECRETS.md](docs/SECRETS.md)。`.env` 已加入 `.gitignore`，不会提交到仓库。

### 3. 一键启动

```bash
make dev
```

该命令会：
1. 加载 `.env` 环境变量
2. 启动 PostgreSQL 和 Redis
3. 启动 auth-rpc、seckill-rpc、mall-rpc、agent-rpc、app、admin 六个服务

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
tail -f logs/app.log
tail -f logs/admin.log

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
- `cmd/admin` 和 `cmd/app` 不直连数据库，数据操作通过 `auth-rpc` / `seckill-rpc`。
