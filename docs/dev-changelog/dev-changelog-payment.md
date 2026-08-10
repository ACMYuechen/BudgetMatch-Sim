# 开发改动日志（dev changelog）

## 2026-07-24 接入用户侧支付宝支付网关（feat/payment）

### 改了什么

- 在 app 网关新增发起支付宝扫码支付和主动查询支付状态两个登录态接口。
- 接入 payment-rpc 客户端，在调用支付服务前校验订单归属、金额和待支付状态。
- 运行 `make api-all` 生成 handler、types、routes 和 Swagger，并补充支付逻辑单元测试。

### 为什么

- payment-rpc 已具备支付基础能力，但此前 cmd/app 没有面向用户暴露支付入口。
- 金额从 mall-rpc 的订单数据读取，避免客户端篡改支付金额；订单归属和状态在网关层提前拦截。

### 影响面

- 新增 `POST /api/mall/orders/:id/pay` 和 `GET /api/mall/orders/:id/pay/query`。
- app 新增 PaymentRpc 配置和客户端，不影响 payment-rpc 原有支付、查询与异步通知逻辑。

## 2026-07-26 支付接口联调修复（fix/payment）

### 改了什么

- 修复支付流水写入 `jsonb` 字段失败的问题，并持久化支付宝二维码。
- 将支付宝未产生交易的状态按待支付处理，同时调整 PaymentRpc 查询超时。
- 迁移支付逻辑测试，并完成本人、非本人、非待支付订单及查询接口的联调验证。

### 为什么

- 避免支付创建返回数据库错误，以及主动查询因支付宝业务状态或默认超时而失败。

### 影响面

- 支付创建和主动查询接口可稳定返回支付状态及二维码，订单归属和状态校验均已验证。

## 2026-07-28 接入支付宝异步通知回调（feat/payment）

### 改了什么

- 在 app 网关新增无需登录的 `POST /api/pay/notify/alipay` 回调入口，并通过 goctl 生成 handler。
- 网关读取支付宝 POST 表单参数，转换为 `HandleNotifyReq` 后透传给 payment-rpc 的 `HandleNotify`。
- payment-rpc 负责支付宝验签、支付流水幂等更新和订单支付状态确认；网关仅按处理结果返回纯文本 `success` 或 `failure`。
- 补充异步通知单元测试，覆盖验签失败不更新流水、验签成功支付成功，以及重复通知不重复确认订单。

### 为什么

- 支付宝支付结果由服务端异步通知，不能依赖客户端轮询或登录态接口完成最终确认。
- 回调请求不携带业务登录凭证，因此该路由必须绕过 `AuthMiddleware`；但验签必须在 payment-rpc 中完成，避免伪造通知篡改支付状态。
- 支付宝仅在收到 `success` 时停止重试，处理失败时返回 `failure` 可以让支付宝后续重发通知。

### 影响面

- 支付宝可直接访问 app 网关完成支付确认，不影响原有创建支付和主动查询接口的鉴权规则。
- 重复回调会复用支付确认的幂等逻辑，不会重复更新订单状态或支付流水。

## 2026-08-04 后台订单支付状态展示与筛选（feat/payment/admin）

### 改了什么

- 在 Mall RPC 的订单结构中新增 `payment_status`、`out_trade_no` 和 `trade_no` 字段。
- 在 Admin API 的订单列表和订单详情中返回支付状态、商户交易号、支付宝交易号及支付时间。
- 新增未支付、已支付和异常三种支付状态：
    - 支付凭证均为空时为未支付；
    - 商户交易号、支付渠道交易号和支付时间均完整时为已支付；
    - 支付凭证只写入一部分时为异常。
- 后台订单列表新增 `payment_status` 查询参数。
- 支付状态筛选在数据库 `Count` 和分页查询前应用，保证筛选后的总数与分页结果一致。
- 补充支付状态计算及 Admin 响应字段映射测试。

### 为什么

- 运营人员需要直接判断订单是否完成支付，并通过商户交易号和支付宝交易号定位支付流水。
- 单独依赖订单业务状态无法区分支付信息完整和部分写入等异常情况。
- 在数据库分页前应用筛选，可以避免先分页再过滤导致列表数量不足、总数错误或遗漏订单。

### 影响面

- `GET /api/admin/mall/orders` 新增 `payment_status` 查询参数。
- Admin 订单列表和详情响应新增：
    - `payment_status`
    - `out_trade_no`
    - `trade_no`
- `pay_time` 继续作为订单支付时间返回。
- 前端可根据 `payment_status` 展示未支付、已支付和异常状态，并使用交易号字段实现复制功能。
- 支付字段的数据写入依赖支付成功回写 Mall 订单的 writeback PR；该 PR 未合并时，未完成回写
  的订单可能显示为未支付或异常。

### 怎么验证的

- 构造未支付、已支付和支付信息不完整订单，确认分别返回支付状态 `1`、`2`、`3`。
- 验证 `payment_status` 筛选后的 `total` 与订单列表结果一致。
- 完成支付宝沙箱支付及主动查询补偿回写测试，确认后台订单详情返回商户交易号和支付宝交易号。
- 执行以下测试并通过：

  ```bash
  go test ./cmd/admin/... ./services/rpc/mall/...
  ```

## 2026-08-03 支付成功回写商城订单状态（feat/payment）

### 改了什么

- 在 payment-rpc 中增加 MallRpc 配置并初始化订单 RPC 客户端，支付流水成功后调用 `ConfirmPayment` 回写商城订单。
- 主动查询和支付宝异步通知统一复用 `markPaid`，正确持久化交易号、买家 ID、支付时间和通知原始数据。
- 使用待支付状态的条件更新保证支付流水并发幂等，并允许已成功流水重试上次失败的 Mall 回写。
- 为 payment-rpc 回写 Mall 时注入短期调用凭据，并补充回写失败的订单号、商户订单号和原始 RPC 错误日志。
- 通过支付宝沙箱和 Cloudflare Tunnel 完成主动查询、异步通知、回写失败重试及重复查询的端到端验证。

### 为什么

- 解决支付宝交易已成功，但 Mall 订单仍停留在待支付状态的数据不一致问题。
- 支付流水更新和 Mall RPC 回写无法组成同一个本地事务，需要保留已成功流水的重试能力，便于失败后补偿。
- 避免重复查询、重复通知或并发请求重复修改支付时间、订单状态或生成支付成功事件。

### 影响面

- 支付宝主动查询和异步通知确认成功后，Mall 订单会由待支付更新为已支付，并写入支付时间和交易号。
- Mall 回写失败时支付流水保持成功语义，接口返回原始错误；后续重复查询可再次触发幂等回写。
- 实测异步通知可在不调用主动查询的情况下将订单更新为已支付；重复查询后支付时间、库存、销量和 Outbox 支付事件数量均未变化。

## 2026-08-10 加固支付 RPC 授权与支付宝回调业务校验（fix/payment）

### 改了什么

- 支付宝异步通知通过 RSA2 验签后，继续精确校验 `app_id`、`seller_id`、`out_trade_no`、`trade_no`、`trade_status` 和 `total_amount`。
- 将支付宝元金额通过字符串解析为整数分进行比较，不使用浮点数；拒绝金额不一致、格式非法和超过两位小数的通知。
- 仅允许 `TRADE_SUCCESS` 和 `TRADE_FINISHED` 状态确认支付，并拒绝已成功流水出现不同支付宝交易号的冲突通知。
- CreatePayment 和 QueryPayment 从认证上下文读取用户身份，校验请求用户、订单归属和支付流水归属，不再只信任请求中的 `user_id`、`order_id`。
- 修复同一用户、同一订单和相同金额的已有支付流水被错误判定为冲突的问题，恢复待支付流水复用和已支付结果幂等返回。
- 为 payment-rpc 调用 mall-rpc 引入独立的短期服务 JWT，校验调用方、签发方、接收方、有效期和签名方法。
- 将 `ConfirmPayment` 限制为 payment-rpc 服务身份调用，普通用户 JWT、错误服务、错误 audience 和错误密钥均无法调用。
- 新增 `PAYMENT_MALL_SERVICE_SECRET` 和 `ALIPAY_SELLER_ID` 配置，payment-rpc 与 mall-rpc 使用相同服务密钥完成内部调用认证。
- 补充支付宝成功通知业务字段校验、跨用户访问、服务 Token、ConfirmPayment 身份限制和事务回滚测试。

### 为什么

- 支付宝通知签名正确只能证明报文来自支付宝，仍需确认通知属于当前应用、当前收款方和当前本地支付流水，避免错误商户、错误金额或错误订单被标记为成功。
- CreatePayment 和 QueryPayment 面向用户开放，不能使用请求字段作为资源归属依据，需要以认证中间件写入的用户身份为准。
- ConfirmPayment 会直接修改订单支付状态和交易凭证，必须与普通用户 JWT 隔离，只允许可信的 payment-rpc 调用。
- 支付回调与主动查询可能重复或并发到达，需要保持支付流水和订单确认幂等，并拒绝不同交易号覆盖已确认结果。

### 影响面

- `POST /api/pay/notify/alipay` 的合法支付宝通知仍返回纯文本 `success`；验签失败或业务字段不匹配时返回 `failure`，不会更新支付流水和订单。
- `POST /api/mall/orders/:id/pay` 和 `GET /api/mall/orders/:id/pay/query` 会拒绝跨用户创建支付或查询支付流水，并使用 NotFound 语义避免泄露目标资源。
- `/mall.OrderService/ConfirmPayment` 不再接受普通用户 JWT，只接受 audience 为 mall-rpc 的 payment-rpc 短期服务 Token。
- 部署时必须为 payment-rpc 和 mall-rpc 配置相同的 `PAYMENT_MALL_SERVICE_SECRET`，并将 `ALIPAY_SELLER_ID` 配置为支付宝商户 PID。
- RocketMQ 消息投递与本次支付授权、回调校验相互独立；本地 broker 地址问题后续单独处理。

### 怎么验证的

- 使用支付宝沙箱和 Cloudflare Tunnel 完成真实异步通知测试，确认 RSA2 验签、AppID、SellerID、金额和交易字段校验通过后，支付流水与商城订单自动更新为成功。
- 核对 payment 与 order 的金额、商户订单号、支付宝交易号和支付时间完全一致，并确认通知原始数据已经落库。
- 使用普通用户 JWT 直接调用 ConfirmPayment，确认返回 `401002:unauthorized.invalid_token`；使用 payment-rpc 服务 Token 调用成功。
- 使用表格测试验证 AppID、SellerID、金额、金额精度、商户订单号、支付宝交易号和交易状态错误时均被拒绝。
- 使用不同认证用户验证 CreatePayment 和 QueryPayment 跨用户请求被拒绝，且不会进入支付宝查询或修改支付状态。
- 重复查询已成功支付，确认支付流水和订单均按相同交易号幂等返回。
- 执行以下测试并通过：

  ```bash
  GOCACHE=/tmp/budgetmatch-go-cache go test -count=1 \
    ./infra/serviceauth \
    ./infra/interceptor \
    ./services/rpc/payment/internal/logic/paymentservice \
    ./services/rpc/mall/internal/logic/orderservice
  ```
