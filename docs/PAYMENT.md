# 支付模块（payment-rpc）配置指南

支付模块对接**支付宝沙箱**，采用**当面付（扫码支付）**：`CreatePayment` 向支付宝预下单并返回二维码码串，用户用沙箱版支付宝 App 扫码付款；支付结果通过**异步通知**或**主动查询**两条路径确认。

服务：`services/rpc/payment`，gRPC 端口 **10007**，etcd key `payment.rpc`，独立的 `payments` 流水表。

> 密钥先留空也能把服务跑起来——`payment-rpc` 会正常启动，仅在发起支付时返回「未配置」错误。拿到沙箱密钥后填入 `.env` 即可。

## 一、需要配置的环境变量

| 变量名 | 是否必填 | 说明 |
|--------|---------|------|
| `ALIPAY_APP_ID` | 是 | 沙箱应用 AppID（形如 `2021000xxxxxxxxx`） |
| `ALIPAY_PRIVATE_KEY` | 是 | **应用私钥**（你自己生成的密钥对中的私钥） |
| `ALIPAY_PUBLIC_KEY` | 是 | **支付宝公钥**（在沙箱平台上看到的、由支付宝提供的公钥，用于验签） |
| `ALIPAY_NOTIFY_URL` | 否 | 异步通知地址，需公网可达。本地无公网时留空，靠 `QueryPayment` 兜底 |
| `ALIPAY_RETURN_URL` | 否 | 同步跳转地址。当面付扫码场景可留空 |

> 注意区分两把公钥：`ALIPAY_PUBLIC_KEY` 填的是**支付宝公钥**，不是你自己的应用公钥。应用公钥是上传给支付宝的，支付宝公钥才是用来验签的。

格式要求：私钥/公钥都填**单行字符串**，去掉 `-----BEGIN ...-----` / `-----END ...-----` 头尾和所有换行。

## 二、如何获取沙箱密钥

1. 登录 [支付宝开放平台 - 沙箱环境](https://open.alipay.com/develop/sandbox/app)。
2. 在「沙箱应用」页面拿到 **APPID**，填入 `ALIPAY_APP_ID`。
3. 接口加签方式选择「**公钥模式**」：
   - 用[支付宝密钥生成工具](https://opendocs.alipay.com/common/02kipl)生成 RSA2 密钥对。
   - 把生成的**应用公钥**上传/粘贴到沙箱平台。
   - 把对应的**应用私钥**填入 `ALIPAY_PRIVATE_KEY`。
   - 把平台显示的**支付宝公钥**复制到 `ALIPAY_PUBLIC_KEY`。
4. 沙箱「沙箱账号」页面提供买家账号与登录密码，配合**沙箱版支付宝 App** 扫码付款测试。

## 三、填写步骤

```bash
# 1. 若还没有 .env，先从模板复制
cp .env.example .env

# 2. 编辑 .env，填入上面四/五个 ALIPAY_* 变量
#    ALIPAY_NOTIFY_URL / ALIPAY_RETURN_URL 本地可留空
```

`services/rpc/payment/etc/config.yaml` 已通过 `${ALIPAY_*}` 占位读取这些环境变量，无需改动配置文件。

## 四、本地验证支付结果（无公网时）

本地通常收不到支付宝的异步通知，用主动查询兜底：

1. 调 `CreatePayment` 拿到 `qr_code`，用工具把码串转成二维码，用沙箱 App 扫码付款。
2. 调 `QueryPayment`（传 `order_id` 或 `out_trade_no`），服务会向支付宝查询交易状态并同步本地 `payments` 流水；支付成功后流水 `status` 置为 `1(success)`。

部署到有公网地址的服务器后，把 `ALIPAY_NOTIFY_URL` 指向网关的通知地址，异步通知（`HandleNotify`）即可自动生效，与 `QueryPayment` 共用同一套幂等确认逻辑。

## 五、当前进度与后续接线（TODO）

已完成（本次「搭结构」范围）：

- `infra/alipay`：支付宝客户端封装（预下单 / 验签 / 查询）。
- `services/rpc/payment`：独立 RPC 服务 + `payments` 流水表，含 `CreatePayment` / `QueryPayment` / `HandleNotify` 三个接口。
- 配置、`.env(.example)`、Makefile 代码生成、`docker-compose`、本地 `dev.sh` 均已接入。

尚未接线（下一阶段，需要时再做）：

- **网关路由**：在 `cmd/app` 暴露 `POST /api/mall/orders/:id/pay`（发起支付）、`GET .../pay/query`（查询）、以及**无鉴权**的 `POST /api/pay/notify/alipay`（接收支付宝异步通知，把表单参数透传给 `HandleNotify`，并向支付宝回写纯文本 `success`）。
- **回写订单状态**：`mall-rpc` 已提供 `ConfirmPayment` RPC，支付成功后还需在 payment 服务的 `markPaid` 流程中可靠调用。旧的 Mall `PayOrder` 模拟接口已移除，发起支付统一走 App 的 `POST /orders/:id/pay` 接口。
