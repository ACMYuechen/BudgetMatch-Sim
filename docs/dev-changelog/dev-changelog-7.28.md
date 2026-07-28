# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把“改了什么、为什么、影响面、怎么验证的”续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序追加在文末，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写到“能让没跟进这段工作的同事看懂”的程度即可，行级细节留给 git diff。

## 2026-07-28 Mall 支付确认与事务 Outbox（feat/mall/confirmpay）

### 改了什么

- 在 mall-rpc 的 `OrderService` 中新增 `ConfirmPayment` RPC，接收订单 ID、用户 ID、支付金额、商户订单号和支付渠道交易号，用于支付成功后确认订单。
- 完善支付确认的事务与幂等逻辑：
  - 在事务中对订单行执行 `SELECT FOR UPDATE`，使同一订单的并发确认串行执行；
  - 校验订单归属、实付金额和订单状态，避免串单、错单及错误金额被确认；
  - 首次确认时将订单从待支付更新为已支付，并记录 `out_trade_no`、`trade_no` 和支付时间；
  - 相同交易号重复确认时幂等返回成功，并通过 `was_already_confirmed` 标识重复确认；
  - 已被其他交易号确认或状态不允许流转时返回明确的业务错误；
  - 订单表中的交易号使用非指针字符串字段，并通过非空唯一索引防止交易号重复绑定到不同订单。
- 新增 Mall 订单事件 Outbox：
  - 支付确认时在更新订单的同一个数据库事务中写入支付成功事件，避免订单提交后 MQ 消息丢失；
  - Outbox 状态包括待发送、发送中、已发送和死信，支持领取次数、最大重试次数、下次重试时间和处理租约；
  - Dispatcher 使用 `FOR UPDATE SKIP LOCKED` 批量领取事件，支持多实例并发消费；
  - MQ 发送失败时按指数退避重新调度，超过最大次数进入死信；进程在发送中退出时，可在租约过期后重新领取；
  - MQ 发送成功后使用状态和领取次数作为更新条件，避免旧任务覆盖新一轮处理结果；
  - 事件包含固定去重键和消息幂等键，投递语义为至少一次。
- Outbox model 基础 CRUD 按项目 `tpls/model` 结构生成，状态流转和事务方法保留在自定义 model 文件中；model 自定义方法仅在业务参数超过 3 个时定义请求结构，不额外包装响应结构。
- 将 MQ 订单事件序列化提取为公共方法，使原有生产者和 Outbox Dispatcher 使用一致的消息格式。
- 移除 Mall RPC 中旧的 `PayOrder` 模拟支付接口、生成客户端/服务端代码和直接修改订单为已支付的模拟逻辑。
- 用户侧发起支付继续统一使用 App 的 `POST /api/mall/orders/:id/pay` 接口，由 App 校验订单后调用 payment-rpc 的 `CreatePayment`，发起支付阶段不会提前修改订单状态。

### 为什么

- 支付平台可能重复回调或由主动查询和异步通知同时确认，支付确认必须具备并发安全和幂等能力。
- 订单状态更新与直接发送 MQ 属于数据库和消息队列之间的双写，服务在数据库提交后、消息发送前退出会造成永久丢消息；事务 Outbox 可以先保证业务数据和事件原子落库，再异步可靠投递。
- 旧 `PayOrder` 只是开发阶段的模拟实现，会绕过 payment-rpc 支付流水、真实交易号和新的 ConfirmPayment/Outbox 链路，继续保留会产生两套互相冲突的支付入口。

### 影响面

- mall-rpc 新增 `ConfirmPayment` RPC；调用方应使用相同的 `out_trade_no` 和 `trade_no` 进行重试，不应在重试时生成新的交易号。
- `mall_orders` 增加商户订单号和支付渠道交易号字段，并新增 `mall_order_outbox` 表；启用 `Database.AutoMigrate` 时由 Mall 服务启动过程自动迁移。
- RocketMQ 未配置或暂时不可用时，支付确认事务仍可成功，事件会保留在 Outbox 中；MQ 恢复并启动 Dispatcher 后继续投递。
- Outbox 提供至少一次投递而不是严格一次投递。后续支付成功事件增加积分、通知等有副作用的消费者时，需要按消息中的 `idempotency_key` 做消费幂等。
- `PayOrder` 已从 Mall proto 中删除，直接依赖该 RPC 的旧客户端属于不兼容变更，需要重新生成客户端并迁移到 App 发起支付接口。
- payment-rpc 当前尚未在支付成功的 `markPaid` 流程中可靠调用 `ConfirmPayment`，支付回调到订单确认的跨服务闭环及服务间鉴权仍需后续接入。

### 怎么验证的

- 执行 `go test ./services/rpc/mall/... ./cmd/app/...`，Mall RPC、Outbox 和 App 相关测试全部通过。
- 执行 `go vet ./services/rpc/mall/... ./cmd/app/...`，静态检查通过。
- Outbox 单元测试覆盖发送成功、发送失败重试、达到最大次数转死信、指数退避和错误信息截断。
- 代码检查确认状态更新均携带当前状态和领取次数条件，终态不会再被 Dispatcher 领取；租约过期重新领取逻辑仍需结合真实 PostgreSQL 补充集成测试。
- 扫描确认 Go、proto 和 API 定义中已不存在旧 `PayOrder` 接口引用，`git diff --check` 通过。
- 当前验证未连接真实 PostgreSQL 和 RocketMQ，数据库行锁、多实例抢占及实际消息投递仍需在集成环境验证。
