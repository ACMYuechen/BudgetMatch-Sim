# 支付模块（payment-rpc）配置与联调指南

支付模块对接**支付宝沙箱当面付（扫码支付）**。`CreatePayment` 负责预下单并返回二维码码串，支付结果通过支付宝**异步通知**或 `QueryPayment` **主动查询**确认，两条路径统一复用幂等支付确认逻辑。

服务路径：`services/rpc/payment`；gRPC 端口：**10007**；etcd key：`payment.rpc`；支付流水表：`payments`。

支付成功后，payment-rpc 使用短期服务 JWT 调用 mall-rpc 的 `ConfirmPayment`，将商城订单更新为已支付并写入商户订单号、支付宝交易号和支付时间。

> 支付宝密钥留空时 payment-rpc 仍可启动，但创建支付会返回未配置错误。完整联调还必须配置收款方 PID、通知地址和 payment-rpc 与 mall-rpc 共用的服务认证密钥。

## 一、需要配置的环境变量

| 变量名 | 是否必填 | 说明 |
|--------|---------|------|
| `ALIPAY_APP_ID` | 是 | 沙箱应用 AppID（形如 `2021000xxxxxxxxx`） |
| `ALIPAY_SELLER_ID` | 是 | 支付宝商户 PID，一般以 `2088` 开头；用于校验通知收款方 |
| `ALIPAY_PRIVATE_KEY` | 是 | 应用私钥，用于请求支付宝时签名 |
| `ALIPAY_PUBLIC_KEY` | 是 | 支付宝公钥，用于验证支付宝异步通知签名 |
| `ALIPAY_NOTIFY_URL` | 异步通知必填 | 公网可达的 `POST /api/pay/notify/alipay` 地址；无公网时可依赖主动查询兜底 |
| `ALIPAY_RETURN_URL` | 否 | 同步跳转地址，当面付扫码场景可留空 |
| `PAYMENT_MALL_SERVICE_SECRET` | 是 | payment-rpc 调用 mall-rpc 的服务 JWT 密钥，建议至少 32 字节 |

`PAYMENT_MALL_SERVICE_SECRET` 必须在 payment-rpc 和 mall-rpc 中保持一致，并与普通用户 JWT 的 `JWT_SECRET` 分离。

> 注意区分两把公钥：`ALIPAY_PUBLIC_KEY` 填的是支付宝提供的**支付宝公钥**，不是应用公钥。应用公钥上传给支付宝，支付宝公钥用于本地验签。

私钥和公钥都填写为单行字符串，去掉 `-----BEGIN ...-----`、`-----END ...-----` 头尾和换行。真实密钥只能放在 `.env` 或部署环境的密钥管理系统中，不要提交到 Git。

## 二、如何获取沙箱配置

1. 登录[支付宝开放平台沙箱环境](https://open.alipay.com/develop/sandbox/app)。
2. 在沙箱应用页面复制 APPID，填入 `ALIPAY_APP_ID`。
3. 在沙箱账号或商户信息页面确认商户 PID，填入 `ALIPAY_SELLER_ID`，不要填写买家 ID。
4. 接口加签方式选择**公钥模式**：
   - 使用[支付宝密钥生成工具](https://opendocs.alipay.com/common/02kipl)生成 RSA2 密钥对；
   - 将应用公钥上传到支付宝；
   - 将对应应用私钥填入 `ALIPAY_PRIVATE_KEY`；
   - 将平台提供的支付宝公钥填入 `ALIPAY_PUBLIC_KEY`。
5. 使用沙箱账号页面提供的买家账号和沙箱版支付宝 App 完成扫码支付。

## 三、填写与启动

如果还没有本地环境文件：

```bash
cp .env.example .env
```

填写示例：

```dotenv
PAYMENT_MALL_SERVICE_SECRET=<至少32字节的独立随机密钥>

ALIPAY_APP_ID=<沙箱应用AppID>
ALIPAY_SELLER_ID=<沙箱商户PID>
ALIPAY_PRIVATE_KEY=<应用私钥单行字符串>
ALIPAY_PUBLIC_KEY=<支付宝公钥单行字符串>
ALIPAY_NOTIFY_URL=https://pay-dev.example.com/api/pay/notify/alipay
ALIPAY_RETURN_URL=
```

payment-rpc 和 mall-rpc 已通过各自的 `etc/config.yaml` 读取 `PAYMENT_MALL_SERVICE_SECRET`，支付宝配置由 payment-rpc 读取，无需把真实值写进 YAML。

修改环境变量后需要重启服务：

```bash
make dev-stop
make dev
```

`notify_url` 在支付宝预下单时确定。修改通知地址或重启 Quick Tunnel 后，必须重新创建商城订单和支付二维码，旧交易不会自动切换到新地址。

## 四、支付链路与授权规则

### 用户发起支付

```text
用户 JWT
  → POST /api/mall/orders/:id/pay
  → CreatePayment
  → 从认证上下文读取 user_id
  → 校验请求用户、订单归属、订单状态和真实支付金额
  → 支付宝预下单并返回二维码
```

CreatePayment 不只信任请求中的 `user_id` 和 `order_id`。认证用户与请求用户不一致、订单不属于当前用户或金额与商城订单不一致时，均拒绝创建支付。

同一订单已有支付流水时：

- 用户和金额一致且流水为待支付：复用原商户订单号；
- 流水已经成功：幂等返回成功状态；
- 用户或金额不一致：返回冲突错误。

### 支付宝异步通知

```text
支付宝
  → POST /api/pay/notify/alipay
  → payment-rpc HandleNotify
  → RSA2 验签
  → 校验 AppID、SellerID、金额和交易字段
  → 标记 payment 成功
  → 使用服务 JWT 调用 mall-rpc ConfirmPayment
  → 标记商城订单已支付
```

回调 HTTP 路由不要求用户 JWT，因为支付宝不会携带业务登录凭证；安全性由支付宝 RSA2 签名和后续业务字段校验保证。

验签成功后仍必须满足：

- `app_id` 与 `ALIPAY_APP_ID` 完全一致；
- `seller_id` 与 `ALIPAY_SELLER_ID` 完全一致；
- `out_trade_no` 非空且对应本地支付流水；
- `trade_no` 非空，且不能与已确认交易号冲突；
- `trade_status` 为 `TRADE_SUCCESS` 或 `TRADE_FINISHED`；
- `total_amount` 精确转换为整数分后与本地流水金额一致。

金额转换不使用浮点数。比如 `9.96` 按字符串解析为 `996` 分；格式非法、超过两位小数或金额不一致时拒绝通知。

### 主动查询与订单确认

`GET /api/mall/orders/:id/pay/query` 从认证上下文读取用户身份，只允许查询当前用户的支付流水。跨用户查询返回 NotFound，避免泄露目标流水是否存在。

支付确认时 payment-rpc 为每次调用生成短期服务 Token。mall-rpc 的 `/mall.OrderService/ConfirmPayment` 只接受 caller 为 payment-rpc、audience 为 mall-rpc 的服务 Token，普通用户 JWT 无法直接调用。

异步通知与主动查询统一复用 `markPaid`：重复通知、重复查询或并发确认不会覆盖不同交易号，也不会重复修改已经确认的订单。

## 五、本地异步通知联调

本地 app 没有公网地址时，可以使用 Cloudflare Tunnel 暴露 `127.0.0.1:10002`。Quick Tunnel 只适合临时开发测试，重启后通常会生成新域名。

```bash
cloudflared tunnel --url http://127.0.0.1:10002
```

将生成的 HTTPS 地址写入 `.env`：

```dotenv
ALIPAY_NOTIFY_URL=https://<当前域名>/api/pay/notify/alipay
```

重启 payment-rpc 后执行无签名探测：

```bash
curl -i -X POST \
  'https://<当前域名>/api/pay/notify/alipay' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'probe=1'
```

预期 HTTP 状态为 `200`，响应正文为 `failure`。这是正确结果：请求已经到达回调处理器，但因为没有支付宝签名而被拒绝。

真实回调测试步骤：

1. 创建一个全新的商城订单，并使用新的幂等键；
2. 创建支付并扫描最新二维码；
3. 支付后不要调用主动查询接口；
4. 观察日志：

   ```bash
   tail -f logs/app.log logs/payment-rpc.log logs/mall-rpc.log | \
     rg --line-buffered 'notify/alipay|HandleNotify|marked paid|ConfirmPayment|payment confirmed|mismatch|verify'
   ```

5. 正常链路应依次出现 `HandleNotify`、`marked paid`、`ConfirmPayment` 和 `payment confirmed`。

## 六、无公网或通知丢失时主动查询

异步通知不是唯一确认路径。无公网、通知延迟或通知投递失败时，调用用户侧查询接口：

```bash
curl \
  'http://127.0.0.1:10002/api/mall/orders/<ORDER_ID>/pay/query' \
  -H 'Authorization: Bearer <用户JWT>'
```

payment-rpc 会向支付宝查询真实交易状态。若支付宝返回支付成功，则使用与异步通知相同的幂等逻辑更新支付流水并确认商城订单。

生产场景仍应保留主动查询作为最终一致性兜底，例如支付页短时间轮询，以及后台定时扫描长时间 pending 的支付流水。

## 七、验证数据库结果

支付成功后，payment 和 order 应同时成功，且金额和交易号完全一致：

```bash
docker compose exec -T postgres \
  psql -U root -d budgetmatch-sim \
  -c "SELECT
        p.order_id,
        p.status AS payment_status,
        p.amount,
        p.out_trade_no,
        p.trade_no,
        p.paid_at,
        p.notify_raw <> '{}' AS notify_saved,
        o.status AS order_status,
        o.pay_amount,
        o.out_trade_no AS order_out_trade_no,
        o.trade_no AS order_trade_no,
        o.pay_time
      FROM payments p
      JOIN mall_orders o ON o.id = p.order_id
      ORDER BY p.created_at DESC
      LIMIT 1;"
```

预期：

- `payment_status = 1`；
- `order_status = 2`；
- payment 与 order 的金额、`out_trade_no`、`trade_no` 一致；
- `paid_at` 和 `pay_time` 不为空；
- 异步通知成功时 `notify_saved = true`。

## 八、自动化测试

支付安全相关测试：

```bash
GOCACHE=/tmp/budgetmatch-go-cache go test -count=1 \
  ./infra/serviceauth \
  ./infra/interceptor \
  ./services/rpc/payment/internal/logic/paymentservice \
  ./services/rpc/mall/internal/logic/orderservice
```

测试覆盖：

- 支付宝通知 AppID、SellerID、金额、商户订单号、支付宝交易号和交易状态；
- 金额格式和精度错误；
- CreatePayment 和 QueryPayment 跨用户访问；
- 服务 Token 的调用方、audience、签名、有效期和用户 JWT 隔离；
- ConfirmPayment 服务身份限制、幂等确认和事务回滚。

PostgreSQL 事务集成测试必须使用独立测试库：

```dotenv
BUDGETMATCH_TEST_POSTGRES_DSN="host=127.0.0.1 user=root password=123456 dbname=budgetmatch_sim_test port=15432 sslmode=disable TimeZone=Asia/Shanghai"
```

不要把集成测试指向日常开发库，避免测试数据影响本地订单。

## 九、常见问题

| 日志或现象 | 含义与处理方式 |
|------------|----------------|
| 探测请求出现 `crypto/rsa: verification error` | 正常，`probe=1` 没有支付宝签名 |
| 真实回调出现 `seller_id mismatch` | `ALIPAY_SELLER_ID` 未配置或不是当前沙箱商户 PID；修正后重启 payment-rpc |
| `record not found` 后 CreatePayment 成功 | 正常，表示该订单此前没有支付流水 |
| CreatePayment 返回 Conflict | 检查运行代码是否已包含金额不等判断修复，并确认已有流水用户和金额与订单一致 |
| 支付宝显示成功但本地仍 pending | 先调用主动查询，再用 `out_trade_no` 到支付宝联调日志查询通知投递记录 |
| 普通用户 JWT 调用 ConfirmPayment 返回 `401002` | 正常，ConfirmPayment 只允许 payment-rpc 服务身份 |
| `mall_order_created` 或 `mall_order_paid` outbox 进入 dead | RocketMQ 投递故障，与回调是否到达、支付和订单是否落库是不同阶段的问题 |

支付通知可能包含买家账号、买家 ID 和签名。排查问题时不要在 issue、PR 或聊天中粘贴完整生产回调参数；请求日志应对敏感字段进行脱敏。
