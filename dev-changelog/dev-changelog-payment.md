# 开发改动日志（dev changelog）

## 2026-07-23 接入用户侧支付宝支付网关（feat/payment）

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


