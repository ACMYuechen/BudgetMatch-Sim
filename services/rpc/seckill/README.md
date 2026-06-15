# seckill-rpc

seckill-rpc 是秒杀业务的 gRPC 服务，负责秒杀活动、活动 SKU 以及秒杀下单的核心逻辑。通过 gRPC 对外暴露 `ActivityService`、`SkuService` 与 `SeckillService`，供 `cmd/admin` 与 `cmd/app` 调用。

## 服务信息

| 项 | 值 |
|----|-----|
| 服务名 | `seckill.rpc` |
| 监听地址 | `0.0.0.0:10004` |
| 协议 | gRPC + Protocol Buffers |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 异步下单 | Redis Streams |

## 提供的 RPC 服务

### ActivityService

| 方法 | 说明 |
|------|------|
| `CreateActivity` | 创建秒杀活动 |
| `UpdateActivity` | 更新秒杀活动信息 |
| `GetActivity` | 查询单个活动详情 |
| `ListActivities` | 分页查询活动列表 |
| `DeleteActivity` | 删除秒杀活动 |
| `PreheatActivity` | 预热活动，将活动下所有 SKU 库存加载到 Redis |
| `OnlineActivity` | 活动上线 |
| `OfflineActivity` | 活动下线 |

### SkuService

| 方法 | 说明 |
|------|------|
| `CreateSku` | 为活动创建秒杀 SKU |
| `UpdateSku` | 更新 SKU 信息 |
| `GetSku` | 查询单个 SKU 详情 |
| `ListSkusByActivity` | 查询指定活动下的 SKU 列表 |
| `DeleteSku` | 删除 SKU |

### SeckillService

| 方法 | 说明 |
|------|------|
| `AcquireToken` | 获取秒杀令牌（前置风控，限流后发放） |
| `SubmitOrder` | 提交秒杀订单（异步排队，0=排队中/1=成功/2=失败） |
| `GetOrder` | 查询秒杀订单结果 |

## 目录结构

```
services/rpc/seckill/
├── desc/
│   └── seckill.proto                 # protobuf 服务定义
├── etc/
│   └── config.yaml                   # 服务配置
├── internal/
│   ├── config/
│   │   └── config.go                 # 配置结构体
│   ├── consumer/
│   │   └── order_consumer.go         # Redis Streams 订单消费者
│   ├── logic/
│   │   ├── activityservice/          # 活动业务逻辑
│   │   ├── seckillservice/           # 秒杀下单逻辑
│   │   └── skuservice/               # SKU 业务逻辑
│   ├── server/
│   │   ├── activityservice/          # ActivityService gRPC server
│   │   ├── seckillservice/           # SeckillService gRPC server
│   │   └── skuservice/               # SkuService gRPC server
│   ├── stock/
│   │   └── stock_manager.go          # Redis 库存与令牌管理
│   └── svc/
│       └── service_context.go        # 依赖注入容器
├── model/                            # GORM 数据模型
│   ├── seckill_activity/
│   ├── seckill_order/
│   └── seckill_sku/
├── pb/                               # goctl 生成的 protobuf 代码
├── client/                           # goctl 生成的 RPC 客户端
└── main.go                           # 服务入口
```

## 数据模型

| 表 | 说明 |
|----|------|
| `seckill_activities` | 秒杀活动主表 |
| `seckill_skus` | 活动 SKU（库存、价格等） |
| `seckill_orders` | 秒杀订单记录 |

本地启动时若 `Database.AutoMigrate: true`，会自动建表。

## 库存与令牌管理

`internal/stock/stock_manager.go` 基于 Redis + Lua 脚本实现高性能库存操作：

| 操作 | 说明 |
|------|------|
| `Preheat` | 预热库存，使用 `SET NX EX` 防止重复加载 |
| `Deduct` | 原子扣减库存，库存不足或 key 不存在返回错误 |
| `Rollback` | 原子回滚库存，用于订单取消或处理失败 |
| `GetStock` | 查询当前剩余库存 |
| `SetToken / GetToken / DelToken` | 秒杀令牌生命周期管理 |

Redis key 规范：

- 库存：`seckill:stock:<activity_id>:<sku_id>`
- 令牌：`seckill:token:<token>`

## 异步订单处理

秒杀下单采用 **Redis Streams** 异步削峰：

- `SubmitOrder` 校验令牌与库存后，将订单消息写入 `seckill:order:stream`。
- `OrderConsumer` 以消费者组 `seckill-order-group` 读取流消息，完成：
  - 创建订单记录
  - 扣减 DB 库存
  - 失败时回滚 Redis 库存
  - 消息认领（claim）机制处理超时未确认消息

该模式避免高并发直接冲击数据库，提升系统吞吐。

## 限流

`ServiceContext` 中内置两层限流：

| 限流器 | 类型 | 作用 |
|--------|------|------|
| `ActivityRateLimiter` | 滑动窗口 | 活动级全局限流，5 秒窗口内最多 1000 次请求 |
| `UserRateLimiter` | 令牌桶 | 用户级限流，容量 5，每 60 秒补充 1 个令牌 |

## 本地运行

确保 PostgreSQL 与 Redis 已启动，然后：

```bash
cd services/rpc/seckill
go run main.go -f etc/config.yaml
```

## 重新生成代码

修改 `desc/seckill.proto` 后，执行：

```bash
goctl rpc protoc services/rpc/seckill/desc/seckill.proto \
  --go_out=services/rpc/seckill \
  --go-grpc_out=services/rpc/seckill \
  --zrpc_out=services/rpc/seckill \
  --style=go_zero
```

## 测试

```bash
go test ./services/rpc/seckill/...
```

## 开发注意

- 业务逻辑写在 `internal/logic/` 下，不要直接修改 `pb/` 与 `client/` 里的生成代码。
- 秒杀下单是异步流程，`SubmitOrder` 返回 `status=0` 表示正在排队，客户端应通过 `GetOrder` 轮询最终结果。
- 活动上线前建议先调用 `PreheatActivity`，将 SKU 库存预热到 Redis。
- 令牌与限流是秒杀防刷的第一层防护，异常流量会直接在 `AcquireToken` 阶段被拒绝。
