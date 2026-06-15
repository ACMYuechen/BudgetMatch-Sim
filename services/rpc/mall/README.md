# mall-rpc

mall-rpc 是商城核心业务的 gRPC 服务，负责商品（SPU/SKU）与订单的领域逻辑。通过 gRPC 对外暴露 `ProductService` 与 `OrderService`，供 `cmd/admin` 与 `cmd/app` 调用。

## 服务信息

| 项 | 值 |
|----|-----|
| 服务名 | `mall.rpc` |
| 监听地址 | `0.0.0.0:10005` |
| 协议 | gRPC + Protocol Buffers |
| 数据库 | PostgreSQL |
| 缓存 | Redis |
| 消息队列 | RocketMQ（可选，未配置时不启动） |

## 提供的 RPC 服务

### ProductService

| 方法 | 说明 |
|------|------|
| `CreateProduct` | 创建 SPU 商品 |
| `UpdateProduct` | 更新 SPU 商品信息 |
| `DeleteProduct` | 删除 SPU 商品 |
| `GetProduct` | 查询单个 SPU 商品详情 |
| `ListProducts` | 分页查询 SPU 列表 |
| `CreateSku` | 为 SPU 创建 SKU |
| `UpdateSku` | 更新 SKU 信息 |
| `DeleteSku` | 删除 SKU |
| `GetSku` | 查询单个 SKU 详情 |
| `ListSkusByProduct` | 查询指定 SPU 下的 SKU 列表 |

### OrderService

| 方法 | 说明 |
|------|------|
| `CreateOrder` | 创建订单（含幂等键去重） |
| `GetOrder` | 查询订单详情 |
| `ListOrders` | 分页查询用户订单列表 |
| `CancelOrder` | 取消订单并回滚库存 |
| `UpdateOrderStatus` | 更新订单状态 |
| `PayOrder` | 订单支付（占位，可扩展对接支付平台） |

## 目录结构

```
services/rpc/mall/
├── desc/
│   └── mall.proto                    # protobuf 服务定义
├── etc/
│   └── config.yaml                   # 服务配置
├── internal/
│   ├── config/
│   │   └── config.go                 # 配置结构体
│   ├── logic/
│   │   ├── orderservice/             # 订单业务逻辑
│   │   └── productservice/           # 商品业务逻辑
│   ├── mq/
│   │   ├── types.go                  # Topic / EventType / OrderEvent
│   │   ├── producer.go               # 订单事件生产者
│   │   └── consumer.go               # 订单事件消费者
│   ├── server/
│   │   ├── orderservice/             # OrderService gRPC server 实现
│   │   └── productservice/           # ProductService gRPC server 实现
│   └── svc/
│       └── service_context.go        # 依赖注入容器
├── model/                            # GORM 数据模型
│   ├── mall_orders/
│   ├── mall_order_items/
│   ├── products/
│   └── product_skus/
├── pb/                               # goctl 生成的 protobuf 代码
├── client/                           # goctl 生成的 RPC 客户端
└── main.go                           # 服务入口
```

## 数据模型

| 表 | 说明 |
|----|------|
| `products` | SPU 商品主表 |
| `product_skus` | SKU 规格与库存表 |
| `mall_orders` | 订单主表 |
| `mall_order_items` | 订单明细表 |

本地启动时若 `Database.AutoMigrate: true`，会自动建表。

## RocketMQ 事件

当订单状态发生变更时，mall-rpc 会发送对应事件到 RocketMQ，供下游系统消费。

### Topic

| Topic | 说明 |
|-------|------|
| `mall_order_created` | 订单创建 |
| `mall_order_paid` | 订单支付 |
| `mall_order_cancelled` | 订单取消 |

### 事件类型

| 类型 | 值 |
|------|-----|
| 创建 | `created` |
| 支付 | `paid` |
| 取消 | `cancelled` |

### 消费者行为

`OrderEventConsumer` 监听上述三个 Topic，目前处理：

- `created` / `cancelled`：根据 SKU 找到对应 SPU，异步删除 Redis 商品缓存。
- `paid`：占位逻辑，可扩展积分、通知、对账等。

当 `RocketMQ.NameServers` 未配置时，生产者与消费者均不会启动。

## 本地运行

确保 PostgreSQL、Redis、RocketMQ（可选）已启动，然后：

```bash
cd services/rpc/mall
go run main.go -f etc/config.yaml
```

## 重新生成代码

修改 `desc/mall.proto` 后，执行：

```bash
goctl rpc protoc services/rpc/mall/desc/mall.proto \
  --go_out=services/rpc/mall \
  --go-grpc_out=services/rpc/mall \
  --zrpc_out=services/rpc/mall \
  --style=go_zero
```

## 测试

```bash
go test ./services/rpc/mall/...
```

## 开发注意

- 业务逻辑写在 `internal/logic/` 下，不要直接修改 `pb/` 与 `client/` 里的生成代码。
- 订单创建、取消等核心流程中已做库存扣减/回滚与幂等控制。
- 缓存失效通过 MQ 异步处理，降低主流程延迟。
