# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把「改了什么、为什么、影响面、怎么验证的」续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序**追加在文末**，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写"能让没跟进这段工作的同事看懂"的程度即可，行级细节留给 git diff。

## 2026-07-15 infra/uuid 通用主键生成器与 API 简化（feat/uuid/hfs）

### 改了什么

- 新增 `infra/uuid` 包，为各微服务 model 层提供统一的业务主键生成能力（此前各服务各自散写）。
- 各 RPC 服务（auth / payment / mall / seckill）model 层的主键生成函数及相应 logic 调用统一接入该包。
- 随后对包做了一次 API 简化，收敛为三个直出函数：
  - `NewUUID()`：标准 UUID v4 字符串；
  - `NewShortUUID()`：固定 22 位、URL 安全的 Base64 编码 UUID v4（保留完整 128 位随机性，仅改编码）；
  - `NewPrefixedShortUUID(prefix)`：前缀 + Short UUID，前缀**原样拼接、不做校验**，分隔符（如 `usr-`）由调用方自带。
- 移除了旧版的前缀校验与生成器闭包 API（`ValidatePrefix`、`NewPrefixedShortGenerator`、`MustNewPrefixedShortGenerator`、`ErrInvalidPrefix` 等），并删除包内 README，注释收敛到 `uuid.go` 的 doc comment。
- 单测按新 API 重写，覆盖 UUID v4 合法性、Short UUID 长度与可逆解码、前缀拼接、批量唯一性。

### 为什么

- 主键生成属于横切能力，放 infra 层复用，避免每个服务重复实现、格式漂移。
- 前缀校验与生成器闭包在实际使用中属于过度设计：前缀都是项目内常量，编译期即可保证合法，简化为纯函数后调用方更直观。

### 影响面

- 全部使用业务主键的 model 层（auth/user、payment/payments、mall 的 products / product_skus / mall_orders / mall_order_items、seckill 的 activity / sku / order）已统一迁移：
  - 每个 model 提供实体命名的主键函数（如 `user.NewUserId()`、`payments.NewPaymentId()`），内联前缀直接调用 `uuid.NewPrefixedShortUUID`；
  - 删除各 model 的 `idPrefix` 常量、`generateID` 变量与 `BeforeCreate` 自动补主键钩子，**主键改为 logic 层创建实体时显式生成**；
  - 相应更新 8 处创建 logic（注册、支付流水、商品/SKU、秒杀活动/SKU、商城下单的订单与订单项）。
- ⚠️ 前缀分隔符由 `_` 改为 `-`（如 `usr_xxx` → `usr-xxx`），仅影响新写入数据，存量 ID 不受影响（主键只做等值匹配，无格式解析）。

### 怎么验证的

- `go test ./infra/uuid/` 通过（含 1 万次 Short UUID 唯一性测试）。
- 全仓 `go build ./...` 通过；`go test ./...` 除 `infra/configcenter`、`infra/dlock` 两个依赖本地 etcd 环境（`ETCD_HOSTS`）的既有失败外全部通过。
