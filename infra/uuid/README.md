# UUID 主键生成改造说明

## 一、为什么要改

项目原来的主键生成方式不统一：

- 用户 ID 使用 32 位随机字符串。
- 商城、秒杀、支付等模块在业务 logic 中直接生成标准 UUID。
- 每个新增接口都要手动填写 `Id`，容易遗漏，也不方便识别 ID 属于哪类数据。

本次改造的目标是统一 ID 格式，并将主键生成职责从业务 logic 移到 model 层。

## 二、新增了什么

在 `infra/uuid` 下新增了通用 UUID 工具和单元测试，支持：

- 标准 UUID，例如 `550e8400-e29b-41d4-a716-446655440000`。
- 固定 22 位的 Short UUID，例如 `VQ6EAOKbQdSnFkRmVUQAAA`。
- 带业务前缀的 Short UUID，例如 `prod_VQ6EAOKbQdSnFkRmVUQAAA`。
- 前缀格式校验和可复用的 ID 生成器。

Short UUID 由完整 UUID v4 编码得到，只改变展示形式，不会截断 UUID 的随机内容。

## 三、ID 生成方式发生了什么变化

改造前，业务 logic 需要主动生成主键：

```go
product := &products.Products{
	Id:   uuid.New().String(),
	Name: in.Name,
}
```

改造后，每个 model 都有自己的前缀生成器，并通过 GORM `BeforeCreate` 自动补充空主键。普通业务只需要填写业务字段：

```go
product := &products.Products{
	Name: in.Name,
}
```

写入数据库时，model 会自动生成类似下面的主键：

```text
prod_VQ6EAOKbQdSnFkRmVUQAAA
```

如果调用方已经传入 ID，model 不会覆盖，兼容消息消费、数据导入和旧数据处理。

## 四、当前前缀约定

| 表 | 前缀 |
| --- | --- |
| `users` | `usr` |
| `products` | `prod` |
| `product_skus` | `psku` |
| `mall_orders` | `mord` |
| `mall_order_items` | `mitem` |
| `payments` | `pay` |
| `seckill_activities` | `sact` |
| `seckill_skus` | `ssku` |
| `seckill_orders` | `sord` |

`product_vectors` 的主键直接使用商城 SKU ID，因此不单独生成。

## 五、特殊场景

大部分记录由 model 自动生成 ID。只有业务在入库前就需要 ID 时，才调用对应 model 的 `NewID()`。

例如商城订单的订单项和事件需要提前引用订单 ID：

```go
orderID := mall_orders.NewID()
```

秒杀订单需要先把订单 ID 写入 Redis Stream：

```go
orderID := seckill_order.NewID()
```

## 六、兼容和使用说明

- 不修改数据库中的旧 ID，新旧格式可以同时存在。
- 新 ID 最长为 28 位，符合当前 `varchar(36)` 和 `max=36` 限制。
- 支付宝商户订单号、秒杀 token、Agent 会话 ID 不是 model 主键，本次没有修改。
- 新增 model 时，需要分配不重复的短前缀，并接入 `NewID()` 和 `BeforeCreate`。
- 普通业务 logic 不应再手动生成 model 主键。
- 不要通过截取 UUID 字符串的方式生成 Short UUID。
