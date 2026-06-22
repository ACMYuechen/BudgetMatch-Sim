# services/ — 业务服务层

services/ 是独立的业务服务部署单元，通过 gRPC 协议对外提供服务。每个服务可以独立部署、独立扩缩容。

## 为什么用 RPC？

| 场景 | 处理方式 |
|------|----------|
| 多 Gateway 共用业务逻辑 | 抽成 RPC 服务，避免代码重复 |
| 需要独立部署/扩缩容 | 拆成 RPC 服务，独立运维 |
| 跨服务调用需要强类型 | gRPC + protobuf 提供契约 |

## RPC 服务目录规范

```
services/rpc/<service>/
├── desc/
│   └── <service>.proto           # protobuf 定义
├── etc/
│   └── config.yaml               # 服务配置
├── internal/
│   ├── config/
│   │   └── config.go             # 配置结构体
│   ├── interceptor/
│   │   └── auth_interceptor.go   # gRPC 拦截器（认证/日志/限流）
│   ├── logic/
│   │   └── <service>/
│   │       └── xxx_logic.go      # 业务逻辑（手写）
│   ├── server/
│   │   └── <service>/
│   │       └── xxx_server.go     # gRPC server 实现（goctl 生成）
│   └── svc/
│       └── service_context.go    # DI 容器
├── model/                        # 业务数据表
├── pb/                           # 生成的 protobuf Go 代码（不要编辑）
├── main.go                       # 服务入口
└── client/                       # RPC 客户端包装（goctl 生成）
    ├── <Aservice>/
    │   └── A_service.go
    └── <Bservice>/
        └── B_service.go
```

> 已落地服务：`auth-rpc`、`seckill-rpc`、`mall-rpc`、`agent-rpc` 均遵循上述目录规范。

## 新增 RPC 服务步骤

```bash
# Step 1: 创建目录结构
mkdir -p services/rpc/<service>/{desc,etc,internal/{config,interceptor,logic/<service>,server/<service>,svc},pb}

# Step 2: 编写 desc/<service>.proto

# Step 3: 生成代码
goctl rpc protoc services/rpc/<service>/desc/<service>.proto \
  --go_out=services/rpc/<service> \
  --go-grpc_out=services/rpc/<service> \
  --zrpc_out=services/rpc/<service> \
  --style=go_zero

# Step 4: 编写 internal/logic/ 下的业务逻辑

# Step 5: 更新 main.go 注册服务
```

参考实现：`services/rpc/agent/` 完整遵循上述步骤，可作为新增 RPC 服务的模板。

## gRPC 拦截器规范

| 拦截器 | 位置 | 作用 |
|--------|------|------|
| AuthInterceptor | `internal/interceptor/` | 从 metadata 提取 token 验证 |
| LogInterceptor | （可选） | 请求/响应日志 |
| RateLimitInterceptor | （可选） | 限流保护 |

> 白名单接口（如登录/注册）在拦截器中跳过认证。
