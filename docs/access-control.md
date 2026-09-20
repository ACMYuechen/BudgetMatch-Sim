# 微服务权限控制现状

核对日期：2026-09-16；Agent 相关增量更新至 2026-09-20 M6.3b Flash 正式配置与本机 `.env` 同步验收（含真实流式、模型限额、流式 RPC/HTTP/前端身份、分段预算、日志、隔离真实存储与恢复、真实登录与浏览器验收，以及显式开发库联调和只读复查边界）。范围：`cmd/app`、`cmd/admin` 和当前 RPC 服务。

本文依据已注册路由、RPC 拦截器、业务逻辑、配置模板和已有测试描述现状，不是目标架构设计，也不表示已通过完整安全审计。代码注释与实现不一致时，以实际执行路径为准。本文不包含真实密钥，不依赖本机 `.env` 的内容。

应用权限与 Kubernetes / Headlamp 的 RBAC 是两套独立体系；安装集群管理界面不会补齐下面的业务权限，也不代表这些微服务已经部署到 Kubernetes。

## 1. 结论与阅读约定

当前采用“HTTP 网关鉴权 + RPC 方法级鉴权 + 部分业务资源归属校验”。已有用户/管理员角色区分，但没有菜单权限、权限点、租户、商户角色、角色授权表等完整细粒度 RBAC。

| 服务 | 网关/方法级控制 | 数据范围控制现状 | 主要待补项 |
| --- | --- | --- | --- |
| `app`（10002） | 公开登录/注册/回调，其余业务接口要求全局用户 | 订单、秒杀请求由网关注入当前用户 ID | 网关检查不能代替 RPC 自身的归属校验 |
| `admin`（10001） | 管理业务接口要求全局管理员 | 管理员可管理全局业务数据 | 没有按运营、财务、商户等划分权限 |
| `auth-rpc`（10003） | 认证接口白名单；用户接口要求数据库角色在 1–199 | `GetUserInfo` 固定为本人 | 4 个用户管理 RPC 缺少管理员判断；禁用状态未参与认证 |
| `seckill-rpc`（10004） | 活动/SKU 配置写入及 `GetSku` 要求管理员，其余要求用户 | 查订单绑定 JWT 用户 | 领令牌、下单仍信任请求 `UserId`；一次性令牌未绑定用户 |
| `mall-rpc`（10005） | 商品写入、Outbox 管理要求管理员；支付确认与商品索引分别要求独立服务身份 | 普通订单接口依赖请求中的 `UserId`；索引仅暴露上架商品最小字段 | 订单归属未绑定认证上下文；`UpdateOrderStatus` 未列入管理员方法 |
| `payment-rpc`（10007） | 创建/查询支付要求用户；回调不要求用户 JWT | 支付流水绑定 JWT 用户；回写订单使用独立服务 JWT | 沿用通用用户 JWT 的角色时效与撤销限制 |
| `agent-rpc`（10006） | 全部推荐/会话 RPC 要求用户；后台索引使用独立只读凭据 | 会话、可选文件工具按认证用户隔离，无管理员跨用户特权 | 索引真实部署联调与一致性仍待验收；MCP 尚无 OS/网络沙箱 |

后文的权限标签：

- **公开**：不要求业务用户 JWT，但仍可能要求密码、验证码或支付签名。
- **用户**：`role` 在 1–199，包含管理员。
- **管理员**：`role` 在 1–99，不只包括枚举中定义的 1、2。
- **服务专用**：只接受符合指定调用方/接收方的服务 JWT。
- **缺口**：当前代码未实现该项校验；建议不代表已经生效。

## 2. 身份、凭据与通用鉴权链路

### 2.1 角色及账号状态

角色定义见 [infra/role/role.go](../infra/role/role.go)：

| 值/范围 | 当前含义 | 实际判定 |
| --- | --- | --- |
| `1` | 超级管理员 | 与其他全局管理员使用相同的范围判断，无额外专属权限 |
| `2` | 管理员 | 同上 |
| `1–99` | 全局管理员范围 | `IsGlobalAdminRole` 返回 true |
| `100` | 普通用户，注册默认值 | 同时属于全局用户范围 |
| `1–199` | 全局用户范围 | `IsGlobalUserRole` 返回 true；101–199 并非自动拒绝 |
| `0`、负数、`200+` | 不属于上述全局角色范围 | 被相应角色检查拒绝，不能当作当前已实现的业务角色 |

账号另有 `StatusNormal=1`、`StatusDisabled=2`，但当前用户名/邮箱/验证码登录、`ValidateToken`、auth-rpc 拦截器均没有检查 `Status`。只修改用户状态为“禁用”不会自动阻止登录或后续调用。角色不合法与账号禁用是不同概念。

依据：[状态常量](../services/rpc/auth/model/user/vars.go)、[登录逻辑目录](../services/rpc/auth/internal/logic/authservice/)、[用户模型](../services/rpc/auth/model/user/users_model.go)、[认证拦截器](../services/rpc/auth/internal/interceptor/auth_interceptor.go)。

### 2.2 普通用户 JWT

- 由 auth-rpc 登录接口签发，签发算法为 HS256，业务 claims 为 `user_id`、`role`、`exp`。
- 各 RPC 配置模板使用同一个 `JWT_SECRET`；auth-rpc 默认 `Expire: 86400`，即 24 小时。实际部署可覆盖配置，本文不推定真实密钥值。
- 通用验证函数检查 HMAC 签名与 JWT 有效性，但没有将允许算法进一步限定为只接受 HS256，也没有统一强制要求用户 JWT 带 `iss`、`aud` 或 `jti`。
- 没有已实现的服务端退出登录、Token 黑名单、Token 版本失效或刷新令牌接口。前端退出仅清理本地登录信息，不撤销已经签发的 JWT。
- 推荐统一使用 `Authorization: Bearer <用户JWT>`。HTTP 中间件对 `Bearer ` 前缀的处理区分大小写；RPC 提取函数会去除空白且不区分该前缀大小写，不应依赖这些宽松/不一致的输入形式。

依据：[JWT 工具](../infra/auth/auth.go)、[auth 配置模板](../services/rpc/auth/etc/config.yaml)、[前端登录状态](../web-ui/src/stores/authStore.ts)。

### 2.3 HTTP 与 RPC 不是同一套角色数据源

```text
浏览器携带用户 JWT
  → app/admin AuthMiddleware
  → auth-rpc.ValidateToken：验签、查用户表，返回当前数据库用户
  → app 检查数据库 role 为 1–199；admin 检查为 1–99
  → 网关注入 user_id、user、原始 token
  → 客户端拦截器将原始 JWT 放入下游 gRPC metadata
  → RPC 再鉴权
      auth-rpc：重新查用户表，检查数据库 role
      其他 4 个 RPC：通常直接检查 JWT 内的 role
  → 业务 logic 执行各自的数据归属/状态校验
```

通用 RPC 拦截器的优先级为：`ServiceMethods` → `NoAuthMethods` → 普通 JWT → `AdminMethods` 或默认用户范围。新方法如果没有加入管理员集合，默认只是“用户可调用”，不是默认管理员或默认拒绝。

`ContextKeyUserId`、`ContextKeyRole`、`ContextKeyToken` 用于 RPC 内部；HTTP 网关注入的是字符串键 `user_id`、`user`、`token`。客户端拦截器兼容两种 Token 来源，只透传已有用户身份，不会凭空赋予服务权限。

因此，角色降级后，HTTP 管理入口会按数据库最新角色拒绝，但已签发的管理员 JWT 若能直达通用 RPC，仍可能在过期前被按旧角色接受；晋升后也可能出现网关通过、下游按旧 JWT 角色拒绝的情况。删除用户同样不会让其他 RPC 主动查库撤销旧 JWT。

依据：[App 中间件](../cmd/app/internal/middleware/auth_middleware.go)、[Admin 中间件](../cmd/admin/internal/middleware/auth_middleware.go)、[ValidateToken](../services/rpc/auth/internal/logic/authservice/validate_token_logic.go)、[通用服务端拦截器](../infra/interceptor/auth_interceptor.go)、[客户端透传拦截器](../infra/interceptor/client_interceptor.go)。

## 3. 两个 HTTP 网关

以下路径均为后端实际路径，不是前端页面路由。App 与 Admin 分别监听 10002、10001；两边的 `GET /api/health` 都公开，不能把健康检查算成管理员接口。

### 3.1 App API

| HTTP 路径 | 方法 | 当前权限与数据范围 |
| --- | --- | --- |
| `/api/health` | GET | 公开 |
| `/api/auth/register` | POST | 公开；邮箱验证码注册，角色固定为 100 |
| `/api/auth/login/username` | POST | 公开；用户名 + 密码 |
| `/api/auth/login/email` | POST | 公开；邮箱 + 密码 |
| `/api/auth/login/code` | POST | 公开；邮箱 + 验证码 |
| `/api/auth/code/send` | POST | 公开；邮箱格式检查、发送频率限制 |
| `/api/user/info` | GET | 用户；返回当前认证用户 |
| `/api/mall/products`、`/api/mall/products/:id` | GET | 用户；不是游客接口，也没有按商户归属隔离 |
| `/api/mall/skus`、`/api/mall/skus/:id` | GET | 用户；商品/SKU 查询 |
| `/api/mall/orders` | POST、GET | 用户；网关将当前用户 ID 传给创建/列表 RPC |
| `/api/mall/orders/:id` | GET | 用户；网关传当前用户 ID 作为归属条件 |
| `/api/mall/orders/:id/cancel` | POST | 用户；网关传当前用户 ID，只取消本人待支付订单 |
| `/api/mall/orders/:id/pay` | POST | 用户；读取并验证本人订单，从订单获取金额 |
| `/api/mall/orders/:id/pay/query` | GET | 用户；检查本人订单和返回支付流水的一致性 |
| `/api/seckill/activities`、`/api/seckill/activities/:id`、`/api/seckill/skus` | GET | 用户；活动和 SKU 查询 |
| `/api/seckill/token` | POST | 用户；网关注入当前用户 ID，获取一次性秒杀令牌 |
| `/api/seckill/orders` | POST | 用户；网关注入当前用户 ID，并传秒杀令牌 |
| `/api/seckill/orders/:order_id` | GET | 用户；RPC 从认证上下文判断订单归属 |
| `/api/agent/recommend` | POST | 用户；只使用当前用户的会话空间 |
| `/api/agent/intent/plan` | POST | 用户；只规划/保存本人需求，不执行模型、检索或推荐 |
| `/api/agent/intent/execute` | POST | 用户；仅执行本人当前已就绪的规划轮次；默认关闭，显式 demo/mall 模式分别使用隔离演示/用户身份商城核验 |
| `/api/agent/recommend/stream` | POST | 同上；`stream_version: 1` 走流式 RPC，省略/0 保留 unary 阶段 SSE；两者都要求 Authorization |
| `/api/agent/conversations` | GET | 用户；只列出本人会话 |
| `/api/agent/conversations/:conversation_id/turns` | GET | 用户；只查看本人会话轮次 |
| `/api/agent/conversations/:conversation_id` | DELETE | 用户；只删除本人会话 |
| `/api/pay/notify/alipay` | POST | 无用户 JWT；原始表单转 payment-rpc 验签及业务校验 |

补充边界：

- 商品列表把请求中的 `status`、`user_id` 作为筛选项传给 mall-rpc；秒杀活动列表也透传 `status`。目前不能把用户端查询描述为“服务端强制只返回上架/上线资源”。下单时的商品/活动状态校验是另一层规则。
- 秒杀网关中间件对路径包含 `/token` 或 `/orders` 的请求按“当前用户 + 完整路径”限流，配置为 60 秒 10 次；实现没有只限定 POST，因此查询订单也会经过该判断。
- 前端请求工具会附加 Bearer Token；管理布局会重新获取用户信息并检查 1–99。页面隐藏或前端路由拦截只是交互限制，不替代后端授权。

依据：[App API 定义](../cmd/app/desc/app.api)、[已注册路由](../cmd/app/internal/handler/routes.go)、[商城网关逻辑](../cmd/app/internal/logic/mall/)、[秒杀限流中间件](../cmd/app/internal/middleware/seckillratelimit_middleware.go)、[依赖组装及限流配置](../cmd/app/internal/svc/service_context.go)、[前端 AdminLayout](../web-ui/src/components/admin/AdminLayout.tsx)。

### 3.2 Admin API

除 `/api/health` 外，当前所有已注册业务接口均使用 Admin `AuthMiddleware`，要求数据库中的角色为 1–99。

| 路径前缀 | 路径与方法 | 管理范围 |
| --- | --- | --- |
| `/api/admin` | GET `/users`；GET / PUT / DELETE `/users/:user_id` | 全局用户列表、详情、信息及角色/状态修改、删除 |
| `/api/admin/seckill` | GET / POST `/activities`；GET / PUT / DELETE `/activities/:id` | 全局秒杀活动 |
| `/api/admin/seckill` | POST `/activities/:id/preheat`、`/activities/:id/online`、`/activities/:id/offline` | 活动预热、上线、下线 |
| `/api/admin/seckill` | GET / POST `/skus`；GET / PUT / DELETE `/skus/:id` | 全局秒杀 SKU |
| `/api/admin/mall` | GET / POST `/products`；GET / PUT / DELETE `/products/:id` | 全局商城商品 |
| `/api/admin/mall` | GET / POST `/skus`；GET / PUT / DELETE `/skus/:id` | 全局商城 SKU |
| `/api/admin/mall` | GET `/orders`、`/orders/:id`；PUT `/orders/:id/status` | 跨用户查订单、执行允许的状态流转 |
| `/api/admin/mall` | GET `/outbox/stats`、`/outbox/events`、`/outbox/events/:id`；POST `/outbox/events/:id/replay` | 全局订单事件统计、详情、重放 |

管理端不区分超级管理员与普通管理员的操作范围，也没有“只能修改自己创建的商品/活动”或“只能由超级管理员授予管理员”的校验。该表说明 HTTP 边界；并不意味着每个下游 RPC 都有同等强度的管理员校验。

依据：[Admin 路由](../cmd/admin/internal/handler/routes.go)、[用户管理 API](../cmd/admin/desc/user/user.api)、[管理网关业务逻辑](../cmd/admin/internal/logic/)。

## 4. 各 RPC 服务的实际权限

表中使用 `Service.Method` 简写；完整方法名为 `/<proto package>.<Service>/<Method>`，例如 `/mall.OrderService/ConfirmPayment`。

### 4.1 auth-rpc：认证与用户管理

使用自己的 [AuthInterceptor](../services/rpc/auth/internal/interceptor/auth_interceptor.go)，不是其他服务的通用管理员方法集合。

| RPC（package `auth`） | 当前入口权限 | logic 内部限制 |
| --- | --- | --- |
| `AuthService.EmailRegister` | 公开 | 验证邮箱/验证码，bcrypt 存储密码，新用户固定 role=100 |
| `AuthService.UsernameLogin`、`AuthService.EmailLogin` | 公开 | 校验密码后签发用户 JWT；未检查账号 Status |
| `AuthService.LoginByCode` | 公开 | 校验 Redis 验证码并尝试删除后签发 JWT；未检查账号 Status |
| `AuthService.SendCode` | 公开 | 验证码 TTL 300 秒；同邮箱发送窗口 60 秒 |
| `AuthService.ValidateToken` | metadata 免鉴权 | 请求体 `Token` 仍须通过验签和用户存在检查；返回数据库用户信息，不在此判断管理员或禁用状态 |
| `UserService.GetUserInfo` | 用户，按数据库 role 判断 | 只使用拦截器注入的用户，忽略请求 `UserId` |
| `UserService.GetUserById`、`UserService.ListUsers` | **当前仅要求用户** | 可按请求查询其他用户；没有额外管理员检查 |
| `UserService.UpdateUserInfo`、`UserService.DeleteUser` | **当前仅要求用户** | 按请求 `UserId` 更新/删除；没有本人或管理员判断 |

关键缺口：后四个方法虽注释为“管理后台接口”，RPC 本身并未要求管理员。`UpdateUserInfo` 还允许设置非零 `Role`、`Status`，没有超级管理员保护或角色取值白名单。若普通用户能够直达 auth-rpc，该缺口存在越权读取、修改、删除用户及提权风险；Admin HTTP 的校验不能保护绕过网关的调用。

依据：[用户逻辑目录](../services/rpc/auth/internal/logic/userservice/)、[更新用户](../services/rpc/auth/internal/logic/userservice/update_user_info_logic.go)、[本人信息](../services/rpc/auth/internal/logic/userservice/get_user_info_logic.go)、[注册](../services/rpc/auth/internal/logic/authservice/email_register_logic.go)、[验证码配置](../services/rpc/auth/internal/logic/authservice/common.go)。

### 4.2 seckill-rpc：秒杀活动、SKU 与订单

| RPC（package `seckill`） | 当前权限 | 备注 |
| --- | --- | --- |
| `ActivityService.CreateActivity`、`ActivityService.UpdateActivity`、`ActivityService.DeleteActivity` | 管理员 | 不按活动创建者隔离 |
| `ActivityService.PreheatActivity`、`ActivityService.OnlineActivity`、`ActivityService.OfflineActivity` | 管理员 | 活动状态操作 |
| `ActivityService.GetActivity`、`ActivityService.ListActivities` | 用户 | App 与 Admin 共用，RPC 不要求管理员 |
| `SkuService.CreateSku`、`SkuService.UpdateSku`、`SkuService.DeleteSku` | 管理员 | 配置写入 |
| `SkuService.GetSku` | 管理员 | 与列表权限不同；当前仅供管理端使用 |
| `SkuService.ListSkusByActivity` | 用户 | App 与 Admin 共用 |
| `SeckillService.AcquireToken` | 用户 | 仍用请求 `UserId` 作为用户限流标识 |
| `SeckillService.SubmitOrder` | 用户 | 仍用请求 `UserId` 作为限流、重复购买判断、订单消息归属 |
| `SeckillService.GetOrder` | 用户 | 从 `ContextKeyUserId` 取身份；非本人订单返回 `SeckillOrderNotFound` |

秒杀令牌不是登录 JWT：当前为 Redis 保存的随机 UUID，有效期 60 秒，值绑定 SKU，提交时使用 `GETDEL` 原子消费。它没有绑定领取用户；领取/提交 RPC 也没有将请求 `UserId` 与 JWT 用户比较。App 网关会填入当前用户，但直达 RPC 仍存在伪造请求用户的边界缺口。

活动/用户限流、活动时间、SKU 状态、库存及重复购买校验已经存在，但它们不能替代身份绑定。管理员查秒杀订单也受“本人”限制，没有通过该 RPC 跨用户查看的特殊权限。

依据：[方法权限表](../services/rpc/seckill/auth.go)、[领令牌](../services/rpc/seckill/internal/logic/seckillservice/acquire_token_logic.go)、[提交订单](../services/rpc/seckill/internal/logic/seckillservice/submit_order_logic.go)、[查订单](../services/rpc/seckill/internal/logic/seckillservice/get_order_logic.go)、[库存/令牌实现](../services/rpc/seckill/internal/stock/stock_manager.go)。

### 4.3 mall-rpc：商品、订单与 Outbox

| RPC（package `mall`） | 当前入口权限 | logic 内部的数据范围 |
| --- | --- | --- |
| `ProductService.CreateProduct`、`ProductService.UpdateProduct`、`ProductService.DeleteProduct` | 管理员 | 全局商品管理，不校验当前管理员是否为商品创建者 |
| `ProductService.CreateSku`、`ProductService.UpdateSku`、`ProductService.DeleteSku` | 管理员 | 全局 SKU 管理 |
| `ProductService.GetProduct`、`ProductService.ListProducts`、`ProductService.GetSku`、`ProductService.ListSkusByProduct` | 用户 | 请求筛选条件不是调用者身份；没有商户/租户隔离 |
| `ProductService.CheckProductCandidates` | 用户 | 一次无缓存 SQL 核对最多 32 个 SKU；默认只读 SPU/SKU 最小事实，显式 `include_demand_category` 才联接运营分类表；不含所有者/内部评价/订单，索引和支付服务 Token 均不能调用，非库存预占 |
| `ProductIndexService.ListProductIndex` | 服务专用 | 只接受 agent-rpc → mall-rpc、用途 `product-index:read` 的独立服务 JWT；logic 再检查调用方/用途，只读未软删除且 SPU/SKU 均上架的索引字段，不返回所有者、内部评价或订单数据 |
| `ProductIndexService.ScanProductIndex`（服务端流） | 服务专用 | 同一独立凭据/调用方/受众/用途；生产流式拦截器验证建流身份，logic 再校验。同一只读数据库快照、30 秒内传输 deadline、每实例单扫描及条数/载荷限制；完成帧只在源事务成功后发送 |
| `OrderService.CreateOrder` | 用户 | 使用请求 `UserId`，未绑定 `ContextKeyUserId` |
| `OrderService.GetOrder` | 用户 | 请求 `UserId` 非空才比较归属；为空不加该限制 |
| `OrderService.ListOrders` | 用户 | 将请求 `UserId` 交给存储层筛选；为空可查询全局范围 |
| `OrderService.CancelOrder` | 用户 | 校验订单归属于请求 `UserId` 且待支付，但未将该 ID 绑定认证用户 |
| `OrderService.UpdateOrderStatus` | **当前仅要求用户** | 不在 `AdminMethods`；请求 `UserId` 非空才查归属，另有状态机限制 |
| `OrderService.ConfirmPayment` | 服务专用 | 只接受 payment-rpc → mall-rpc 服务 JWT，再核对订单归属、金额、交易号和状态 |
| `OrderService.GetOrderOutboxStats`、`OrderService.ListOrderOutbox`、`OrderService.GetOrderOutbox`、`OrderService.ReplayOrderOutbox` | 管理员 | 全局事件管理；重放另有业务校验 |

必须区分的三点：

1. App 订单接口确实从认证上下文填入本人 `UserId`；但 mall-rpc 的这些普通订单方法没有统一强制使用认证上下文。不能据此宣称“直达 RPC 也只能操作本人订单”。
2. `UpdateOrderStatus` 虽然只挂在 Admin HTTP 路由，却没有 RPC 管理员限制。状态机允许的流转包括待支付 → 已支付，因此 `ConfirmPayment` 的独立服务鉴权不代表所有“将订单改为已支付”的路径都只能由 payment-rpc 使用。
3. 创建订单的 Redis 幂等键为 `mall:idempotency:<请求key>`，未包含用户 ID；缓存命中直接返回订单 ID，未再次核对归属。这是另一项跨用户隔离缺口，即使通过 App 网关也不能仅靠网关注入 `UserId` 消除。

依据：[服务注册](../services/rpc/mall/main.go)、[方法权限与密钥隔离](../services/rpc/mall/internal/config/auth.go)、[只读索引查询](../services/rpc/mall/model/product_index/product_index_model.go)、[订单逻辑目录](../services/rpc/mall/internal/logic/orderservice/)、[查订单](../services/rpc/mall/internal/logic/orderservice/get_order_logic.go)、[改状态](../services/rpc/mall/internal/logic/orderservice/update_order_status_logic.go)、[创建订单](../services/rpc/mall/internal/logic/orderservice/create_order_logic.go)、[幂等键](../services/rpc/mall/internal/logic/orderservice/common.go)。

### 4.4 payment-rpc：支付及可信回调

| RPC（package `payment`） | 当前入口权限 | 额外控制 |
| --- | --- | --- |
| `PaymentService.CreatePayment` | 用户 | 请求 `UserId` 必须等于 JWT 用户；以该身份向 mall 取订单，再核对订单 ID、归属、金额、状态及已有流水 |
| `PaymentService.QueryPayment` | 用户 | 按订单号/交易号定位流水后，要求流水 `UserId` 等于 JWT 用户；不赋予管理员跨用户查询特权 |
| `PaymentService.HandleNotify` | 无用户 JWT | payment-rpc 内部使用支付宝公钥验签，核对应用、收款方、交易、金额和状态 |

回调路径为：

```text
支付宝表单
  → POST /api/pay/notify/alipay（公开 HTTP）
  → PaymentService.HandleNotify（用户 JWT 白名单）
  → DecodeNotify 验签 + app_id/seller_id/out_trade_no/金额校验
  → 成功通知还检查 trade_no、成功状态及已确认交易一致性
  → 更新支付流水
  → 独立服务 JWT 调用 mall.OrderService.ConfirmPayment
  → 订单归属/金额/交易号/状态校验 + 幂等确认 + Outbox
```

主动查询确认支付同样走服务 JWT 回写，不把普通用户 JWT 当成支付确认凭据。公开回调不等于任意调用者可以确认支付；同时，它也没有基于网关服务身份的来源限制，主要可信依据是签名及业务字段。

当前 payment-rpc 实际依赖 mall-rpc，创建支付会重新读取订单，不能沿用“payment-rpc 不依赖 mall、只信任网关金额”的旧描述。

依据：[支付服务入口](../services/rpc/payment/main.go)、[创建支付](../services/rpc/payment/internal/logic/paymentservice/create_payment_logic.go)、[查询支付](../services/rpc/payment/internal/logic/paymentservice/query_payment_logic.go)、[回调](../services/rpc/payment/internal/logic/paymentservice/handle_notify_logic.go)、[支付宝封装](../infra/alipay/alipay.go)、[支付专题文档](PAYMENT.md)。

### 4.5 agent-rpc：推荐、会话及工具权限

| RPC（package `agent`） | 当前权限 | 数据范围 |
| --- | --- | --- |
| `RecommendService.Recommend` | 用户 | 用户 ID 仅从认证上下文取得，按用户 + 会话执行推荐和记忆读写 |
| `RecommendService.RecommendStream` | 用户 | stream 拦截器在首次接收请求前验证用户 JWT 和 ≤30 秒的传输 deadline；共用 unary 会话/轮次隔离及原子保存，final 仅在保存成功后发送 |
| `RecommendService.PlanDemand` | 用户 | 同样仅信任认证上下文；显式变更本人需求状态，不接受模型自行放宽条件 |
| `RecommendService.ExecuteDemand` | 用户 | 只执行本人会话中匹配 `plan_turn_id` 的最新就绪规划；不接收新需求/预算覆盖，不调用旧 Agent 或兜底 |
| `RecommendService.ListConversations` | 用户 | 只列出本人会话 |
| `RecommendService.ListConversationTurns` | 用户 | 在本人命名空间查会话；不存在或他人会话返回 `NotFound` |
| `RecommendService.DeleteConversation` | 用户 | 只删除本人会话；未命中时返回 `deleted=false`，不是越权删除 |

RPC 请求不接收可覆盖身份的 `user_id`。PostgreSQL 会话采用 `(user_id, conversation_id)` 复合身份，Redis/内存实现也按用户分区。同一 `conversation_id` 可以在不同用户空间出现，并不意味着共享会话。管理员使用这些接口仍只能访问自己的会话。

`RecommendStream` 在 M5.1 生命周期流上增加 M5.2 Eino Stream 和脱敏工具事件，M5.3a 新网页显式选择 v1 后经网关接入；旧请求仍调用 unary。它以认证后的传输 context 执行并传播取消；伪造 `user_id` metadata 或同名 string context key 不生效，后台索引服务 JWT 不能调用。App 的 Agent 流式客户端从可信 context 透传 Token，覆盖已有 authorization metadata；不修改原 deadline。HTTP 入口另限制总期限不超过 30 秒，并继承更短期限。JWT 在建流时验签，不逐事件重新验证过期/撤销。完成重放只发已保存结果和 done，不再调用工具；提交已成功但响应丢失时用相同请求重试。异常/慢连接、原子完成和公开错误边界见 [M5.1 协议](agent.md#104-m51-已交付的-rpc-生命周期流)。

公开 `answer.delta` 不是 ReAct 消息直通：私有编排完整有界分类，另用禁用工具的模型仅接收预算/件数/总价四个数字并输出临时解释，不接收查询、历史、商品名/ID、文件或工具正文。只转发 Content，不转发隐藏推理/元数据；提示要求不等于能保证自然语言不会幻觉。工具事件只带本地关联 ID、脱敏名称、状态/耗时/有限错误码。增量不持久化，校验/保存前不发 final；开始流式执行后的错误不得触发整轮兜底重跑。次数/载荷、串行工具及零队列背压边界见 [M5.2](agent.md#105-m52-模型增量工具事件与有界背压)。不增加工具权限，也不承诺未保存轮次重试时工具副作用恰好一次。

HTTP v1 同步转发有界公开事件，验证身份/序号/工具关联，只在匹配终态及正常 RPC EOF 后转发 final/done；网页也等待成功 done 与正常 HTTP EOF，失败清除临时解释和工具状态。解释是普通文本节点，不执行 HTML，不作为购买依据。请求 16 KiB、单帧 256 KiB、累计 1 MiB，底层写期限覆盖 handler 返回后的 flush；不能设置期限的 writer 在调用 RPC 前拒绝。旧协议按请求预选或按旧网关响应在同次请求中消费，不在 v1 出错后重跑 unary。新逻辑日志不记录请求正文或原始错误，这不等于已审计/清理 go-zero verbose、代理或其他服务的日志。兼容矩阵、Accept 精确匹配和部署限制见 [M5.3a](agent.md#106-m53a-网关与网页版本化接入)。

M6.3b 的[本机代理回归](../cmd/app/internal/logic/agent/agent_stream_proxy_test.go)增加 Go 反向代理与真实 loopback TCP gRPC 链路，复用网关逻辑、日志/超时中间件及 RPC JWT/期限拦截器。HTTP 身份由测试注入、RPC 输出由受控替身生成，不能算完整 HTTP/Auth 服务鉴权或数据库持久化验收。测试确认迟到错误不转发成功终态，客户端/代理断连可取消上游，代理停止读取时实际 socket 写期限生效；每个请求仅调用一次 stream、零 unary 回退。所有监听与故障注入均为测试自行创建/关闭，不访问部署代理、真实模型或业务库，也不扩展任何现有权限。

M5.3b 在同一原始期限内给生成设置更早的子 deadline，为最终核验/保存及传输留出时间；保留持锁 context 的连接/租约值，不更换身份或绕过锁。生成期限耗尽的迟到结果不得落库/兜底，已确认提交后断连仍可同 ID 重放。请求级汇总只记录随机 execution_id、安全身份标签、有限路径/错误码、计数与耗时；模型 Usage 缺失/不合法/未 EOF 为未知，费用未配置时始终 unknown。记录器请求隔离、最多 9 条模型元数据，结束后冻结，不保留提示词、隐藏推理、工具正文或凭据，不增加高基数指标标签。此处不代表真实 DB 锁清理、SDK 内部重试/计费或所有服务日志均已审计，完整口径见 [M5.3b](agent.md#107-m53b-分段预算与请求级用量追踪)。

M6.1 修复 Redis-only 旧持有者在租约过期后仍可写入的边界：租约保留 2 分钟，等待/执行 context 最长 90 秒且不延长调用方期限，锁令牌绑定存储对象与用户/会话；保存、删除、追加和标题写入通过同连接 WATCH/令牌读取/MULTI/EXEC 条件提交。失效租约不自动重获锁，原令牌只能清理自身锁；清理 context 单独限制 5 秒，但不等于强杀底层 I/O。此令牌是内部协调凭据，不替代 RPC JWT 或数据归属校验。单进程双 Service + miniredis 验证不是实际多进程、Redis Cluster/切主、PostgreSQL 事务或跨系统副作用验收；共享 Redis-only 写实例需全部包含该保护，旧写二进制混跑不作安全承诺。详见 [M6 本地证据与部署边界](agent.md#111-m61-本地故障矩阵与-redis-提交保护)。

M6.2a 的独立验收包不进入运行时服务，也不改变上述 JWT/用户身份策略。真实入口必须显式给出私有 JSON 与匹配的 run ID，只允许字面量 loopback 非默认端口、固定专用 PG 用户/派生库名、独立 Redis DB 0，两个存储先通过本次所有权标记校验；拒绝 `PG*` 环境覆盖，不加载 `.env` 或旧测试 DSN。子进程不继承部署环境，测试口令经 stdin 传递而非命令行/日志。标记仅防误连，不是操作业务数据的授权；会话表/索引写入、删除及 TTL 注入只能发生在另行确认的新建可丢弃实例。M6.2a 的 8 项 OS 多进程框架用例共享 miniredis，后续 M6.2b 已另行实跑，详见 [入口及影响范围](agent.md#113-m62a-多进程验收入口与真实存储运行条件)。

M6.2b 获用户授权后，通过[隔离运行器](../scripts/agent-storage-acceptance.py)创建本机独立 PG/Redis 并完成故障、重启和备份恢复验证，未连接现有业务库或启用部署模式。新的 PG 测试角色禁用 superuser/建库/建角色能力，管理身份仅用于本次新集群的初始化；这不代表已收紧现有服务数据库账号。恢复测试预装 pgvector，备份/恢复排除 owner、ACL、comments，不提升业务测试角色以修改管理员所属扩展，因此不证明生产权限恢复。临时实例已停止，私有配置/口令与备份仅留 `0700` 临时目录，不提交仓库；原有集成测试只获得另一个本次新建空库的显式 DSN，并清理它们自己的合成表/schema。授权范围不延伸到外部模型、部署服务或业务数据，详见 [M6.2b 证据与限制](agent.md#114-m62b-独立真实存储重启与备份恢复验收)。

2026-09-20 用户先纠正业务目标为远程库，最新又明确要求新建本机库测试并保留记录。此前“已确认 `.env` 本地测试库”的错误理解仍不作为依据；本次使用独立新库与私密配置，不覆盖清理性测试 DSN。新建库授权仅用于本次空库初始化/测试账号与自有测试进程，不包含已有库清理/迁移/提权、既有服务变更或付费调用。远程历史检查仍为认证失败、无业务 SQL，本轮没有再次访问，见 [开发库联调约定](agent.md#115-开发库联调与记录保留约定)。

[本机保留库联调](agent.md#1110-m63a2-新建本机保留库与真实-rpc-联调)新建仅绑定 loopback、使用 SCRAM 的独立 PG，专用 DB 角色非 superuser、无建库/建角色权限；演示账号为普通用户，生产 Model 初始化三张新表后关闭自动迁移。保留两轮记录，真实 Agent TCP RPC 验证缺少 JWT 被拒绝、跨用户列表为空/详情 NotFound、同用户 unary/stream 重放不改历史。测试 JWT 本地签发，不能视为 Auth 登录或 HTTP/页面权限验收。自有临时 RPC 已停止，正常重启后的新数据库继续运行并保留数据；口令、配置和备份仅在仓库外私密目录，不代表现有数据库账号或部署权限发生变更。

随后完成[真实登录与浏览器验收](agent.md#1111-m63a2-真实登录与保留库浏览器验收)：新增第二个普通启用账号，不改原账号或数据库角色；浏览器经生产 Auth 登录获取 JWT，经网关和 Agent RPC 读取/写入同一保留库。匿名及错误密码返回 401，两个真实账号互查历史返回 404/列表不包含，退出切换不残留旧历史；页面两轮 SSE、刷新与同 ID 重放通过。该步结束时保留 2 账号 / 2 会话 / 4 轮，原历史快照不变。独立入口默认不运行，要求显式写入授权、私密文件及 loopback origin；这些保护不能替代操作者核对上游库/模型配置。关闭认证 trace/HAR/video，但不宣称 Auth/网关日志已全面脱敏，原始输出只留私密目录。临时服务已停止，仅新库保持运行；该步范围不含管理权限、真实商城/模型、部署代理或现有服务变更。

M6.3b 获本轮最多 10 次推理请求 / 人民币 5 元的本机隔离验收授权，计费或预算不能可靠确认时不调用。先从 `.env` 只选取四个 `LLM_*` 字段进行一次 HTTPS 模型列表预检，未发送提示词；旧 `deepseek-chat` 不再列出后，用户明确选择 Flash，才执行[本机真实流式验收](agent.md#1113-m63b-flash-真实流式与取消验收)。仅临时配置改用非思考模式，`.env` 不变。真实 Key 留在固定 DeepSeek 出站保护层；Agent 只持有随机本地凭据，测试服务仅监听 loopback。

保护层对每次实际出站先持久化请求次数与完整上下文费用预留，失败/重试同样占用次数，只有有效 Usage 加正常 EOF 才核减多余预留；取消不等于停止计费。累计 8 次调用含前期失败，7 次 Usage 已知、1 次未知，总保守上界 2.134688 元，不当作实际账单。生产修复只把解释流改为本次调用显式清空工具 schema 并禁止工具调用，仍只发送四个数字、不传入查询/历史/商品正文，不扩大工具或用户权限。

通过真实 Auth 普通账号在同一本机保留库新增 1 个合成会话 / 1 轮完成结果，现为 2 账号 / 3 会话 / 5 轮；重放无模型调用，取消无完成记录，原演示历史摘要一致。只读复查、服务就绪与阶段失败均有私密证据；未迁移、清库、更改角色或既有服务，自有服务已停止。该授权不包括部署代理、远程库、Mall/Embedding/MCP、生产模型配置迁移或其它质量实验；不因重启就绪后的成功声称零中断，也不因慢读小响应通过声称真实饱和背压通过。

后续[正式配置接入](agent.md#1114-m63b-flash-正式配置接入与离线迁移检查)只改应用、示例与离线渲染：`deepseek-flash` 要求 `Model.Thinking=disabled`，并在任何数据库、Mall 客户端或模型初始化之前校验。不开放任意额外请求参数，也不增加工具权限；普通兼容模型不附带 Flash 字段，关闭 Provider 后保留配置不触发模型调用。K8s 仅新增可选的 `LLM_THINKING` Secret 引用，不把 API Key 或其它原有必需引用改为可选。验收保护层仅额外接受精确的 `{"type":"disabled"}`，仍拒绝思考模式及额外参数，调用/费用、取消与 Unknown 预留规则不变。本轮 `.env`、部署 Secret 和既有服务未变，无真实模型/数据库调用；代码支持不构成生产配置切换或额外费用授权。

用户随后明确允许同步环境参数并要求 `.env` 可用，才执行[本机实际配置验收](agent.md#1115-m63b-env-同步与真实配置验收)：只改模型名/非思考模式、缺失可选键和独立测试 DSN，不自动轮换现有 JWT、Payment 或第三方密钥。测试库在已授权的新建本机 PG 中另建，`budgetmatch_test` 无 superuser/建库/建角色/复制/绕过 RLS 权限，未更改已有角色；新测试库取消 PUBLIC 默认访问，保留演示数据不交给清理性测试。真实 SDK 最小请求从 `.env` 读取模型配置，实际 Key 仍只由出站保护层发送到固定 DeepSeek 地址；只新增 1 次推理并继承前 8 次账本，总 9 次 / 保守费用上界 2.134714 元，旧取消费用仍保留。未切换部署 Secret、操作业务部署或扩大模型授权。

M6.3a1 新增独立 `dev-records` 工具，本机模式显式选择 `.env` 的单个 DSN 或配置文件的 `Database.DSN`，核对库名与 loopback 目标；预检连接由数据库强制只读，不泄露口令或自动迁移。写入另须指定已有启用账号及独立 run ID，事务内再次核对账号/会话锁，保留带演示标识的两轮历史；不改账号、密码或角色，不接入 LLM/Mall/Embedding。该工具使用操作者显式选择的数据库凭据，**不是 HTTP/RPC 用户鉴权入口，也不应作为远程管理接口暴露**。本机授权仍不能访问远程；新建本机库保留记录与上述页面验收是独立执行的步骤，详见 [开发库预检入口](agent.md#116-m63a1-开发库只读预检与保留演示会话入口)。

M6.3a2 的本地增量新增互斥的 `-verify-demo`，仍需显式目标/账号/run ID；只在服务器强制只读、一致性快照中检查既有记录，不创建账号、执行该库上的 Service 写入或做缺失修复。写入成功后也关闭原连接再独立只读确认；复查失败不会回滚已确认的提交或自动重写。仅有 SELECT 权限的凭据可以复查完整记录，写入路径仍要求原有结构检查；该能力不授予/调整现有数据库权限，不代表 HTTP 鉴权或页面联调已验收。结果口径见 [只读复查](agent.md#117-m63a2-提交后确认与只读复查)。

开发库工具的 `diagnostic` 仅暴露固定阶段/原因码，将系统网络权限、PostgreSQL 认证拒绝与 SQL 权限不足分开，不回显原始驱动/服务端错误或保留敏感错误链。它不提供提权、改口令/HBA、自动改库、扫描网络或失败重试能力；用户允许尝试其他连接方式不等于允许修改数据库权限或启动现有服务。

M6.3a2 另增显式 `-allow-remote-dev-db`，与本机授权互斥：只接受独立私密 YAML（无 group/other 权限），不接受 dotenv 测试源；`-expect-address` 固定单个非 loopback IP/端口，`-expect-db` 核对实际库。远程要求 `sslmode=verify-full` 和显式 CA 来源，不支持明文降级、DNS、多主机或客户端证书，不继承默认口令/客户端证书文件。默认仍只读，写入和复查沿用原有账号/run ID/事务保护，不新增 SQL 权限。证书失败仅报告 `tls_verification_failed`，不输出证书或服务端私密信息；当前真实服务器未启用 TLS 且现有凭据认证失败，代码交付不是连接已就绪或业务环境安全验收。用法与证据范围见 [远程连接保护](agent.md#118-m63a2-显式远程目标与-tls-连接保护)。

结构化执行由 `DemandExecution.Mode` 控制：缺省/`disabled` 拒绝新执行；`demo` 要求无 MallRpc，`mall` 要求已配置 MallRpc；非法值/依赖组合在外部初始化前拒绝。mall 模式使用独立有界关键词链和两次用户身份核验，不用后台索引凭据或演示标签。分类请求要求版本握手和逐项分类事实，旧 Mall/缺表/错误证据均失败，不回退旧契约。规划、执行、旧推荐共用用户/会话锁和轮次命名空间；执行要求当前规划就绪且版本匹配，待澄清或过期规划不能绕过。已完成的同请求重放原结果，不重查商品；关闭/切换模式也不把历史快照标记为实时。私有规划标记和请求指纹不进入公开会话状态，旧 Recommend/SSE 保护不解除。

`product_demand_categories` 是 Mall 维护的 SPU 分类，只有显式分类核验查询读取；没有面向用户/模型的分类写接口，不修改管理员现有商品 CRUD 权限。本次只提供独立迁移文件，不自动迁移、赋权或回填。部署时须审核分类写入流程及数据库权限，不能因业务代码只读就声称现有数据库账号已收权；真实分类准确性/权限验收尚未完成。迁移、版本递增和启用边界见 [M4.2c](agent.md#9-m4需求驱动的受约束组合推荐)。

在线推荐调用 mall 商品 RPC 时继续透传当前用户 JWT。后台 RAG 同步已改用独立客户端访问专用索引 RPC，每次签发短期服务 Token；不复用用户身份、支付密钥或管理员角色。Agent 配置了 RAG 却缺少合法索引凭据时，在初始化数据库/Embedding 前拒绝启动；Mall 未配置索引密钥时只拒绝该专用接口，不将其设为免鉴权。已完成本地签名/权限与内存 gRPC 测试，尚未启动真实 Mall＋数据库＋Embedding 的完整 RAG 链路。

LLM 工具权限需与会话权限分开理解：

- 文件工具默认关闭；Linux 下启用后使用 `<Workspace>/users/<SHA-256(user_id)>`，身份仅来自认证上下文。请求持有目录根句柄，拒绝路径越界、非普通文件、末级符号链接和硬链接读取；读写只允许 `.json/.md/.txt` 子集，均有大小上限，目录/新文件权限分别为 0700/0600。
- `write_file` 另需操作员 `AllowWrite` 和本轮原始消息首行 `/save <相对路径>`，只能一次创建指定文件、不覆盖。历史、模型和工具结果不能产生保存授权。同用户不同会话共享文件；管理员没有跨用户特权。文件发布与会话事务分离，不保证副作用 exactly-once。
- MCP 默认关闭；只有非空精确 `AllowedTools` 白名单且工具声明只读时才暴露。启动经审计的绝对路径程序，不继承服务密钥、不转发认证 Header/Meta；临时目录、超时与进程组清理减少泄露及残留风险。没有按用户/角色细分的 MCP 清单或独立调用审批。
- 以上不是独立操作系统/出网沙箱；只读声明需要信任服务端，MCP 不继承本地文件限制，仍可访问服务 OS 身份可访问的资源。文件正文会进入配置的模型上下文，不应放入凭据。旧共享文件不自动迁移，删除会话不清理用户文件。
- Agent 工具记录和自有日志不再记录正文、原始错误；历史响应/幂等重放也过滤旧工具详情，但不追溯改写数据库或旧日志。其他服务日志的凭据风险仍保留。具体配置与限制见 [Agent 开发文档](agent.md#28-文件工具与-mcp-权限)。

依据：[认证身份提取](../services/rpc/agent/internal/logic/recommendservice/common.go)、[会话 RPC 逻辑](../services/rpc/agent/internal/logic/recommendservice/)、[会话模型](../services/rpc/agent/model/conversation_memory/conversation_memory_model.go)、[依赖组装](../services/rpc/agent/internal/svc/service_context.go)、[后台同步](../services/rpc/agent/internal/rag/syncer.go)、[文件工作区](../services/rpc/agent/internal/filetools/workspace.go)、[MCP 适配](../services/rpc/agent/internal/agent/recommend/llm/mcp.go)。

## 5. 服务之间如何传递权限

| 调用链 | 当前身份机制 | 是否具有独立服务身份 |
| --- | --- | --- |
| App/Admin → auth-rpc.ValidateToken | 用户 JWT 放请求体；该方法 metadata 免鉴权 | 否，不验证调用网关是谁 |
| App/Admin → 受保护 RPC | 透传原始用户 JWT | 否，目标服务看到的是用户 |
| agent-rpc 在线推荐 → mall 商品 RPC | 透传当前用户 JWT | 否 |
| agent-rpc 后台 RAG → mall.ScanProductIndex | 每个快照流签发专用用途的短期服务 JWT；不回退旧实时分页 | 是，只读索引，独立密钥；旧 ListProductIndex 仍保留同等服务鉴权 |
| payment-rpc → mall.GetOrder | 显式透传当前用户 JWT | 否，用于以本人身份核对订单 |
| payment-rpc → mall.ConfirmPayment | 每次签发短期服务 JWT | 是，沿用原支付服务密钥与身份规则 |
| 秒杀/商城 → RocketMQ → 消费者 | 消息载荷、Topic/Tag 等业务约定 | 不经过用户 JWT/RPC 拦截器；仓库客户端配置未接入 MQ 凭据/消息签名授权 |

支付服务 JWT 的具体规则：

- 使用 `PAYMENT_MALL_SERVICE_SECRET`，由 payment-rpc、mall-rpc 配置为 `ServiceAuth.Secret`；配置要求与 `JWT_SECRET` 分离，但代码没有统一检查两者必不相等。
- 当前调用 TTL 为 1 分钟，签发 `service`、`iss`、`sub`、`aud`、`exp`、`iat`、`nbf`、`jti`。
- 验证固定 HS256，要求 `service/iss/sub` 均为 `payment-rpc`，受众包含 `mall-rpc`，检查时间有效性并要求 `exp`、`iat` 存在。
- 验证成功注入 `ContextKeyServiceName` 和 `ContextKeyServicePurpose`（旧支付 Token 的用途为空）；`ConfirmPayment` logic 继续检查调用方，并执行订单校验。
- `jti` 是随机 ID，当前没有用它建立一次性消费记录；不能把短期 JWT 写成“不可重放凭据”。订单幂等是独立机制。
- 这是共享密钥认证，不是 mTLS，也不是覆盖所有微服务的统一服务身份平台。普通用户或管理员 JWT 都不能直接通过 `ConfirmPayment` 的服务认证分支。

依据：[服务 JWT 实现](../infra/serviceauth/service_auth.go)、[支付上下文构建](../services/rpc/payment/internal/logic/paymentservice/common.go)、[商城二次调用方校验](../services/rpc/mall/internal/logic/orderservice/confirm_payment_logic.go)、[MQ 配置结构](../infra/rocketmq/config.go)、[MQ 客户端](../infra/rocketmq/rocketmq.go)。

Agent 索引服务 JWT 的额外边界：

- 双方 `IndexAuth.Secret` 对应 `AGENT_MALL_INDEX_SECRET`；至少 32 字节且无首尾空白。Mall 启动时拒绝与用户/支付密钥相同的配置，Agent 拒绝与用户密钥相同的配置；长度校验不等于密钥随机性证明。
- `ServiceMethodPolicy.SecretName` 选择命名密钥，缺失时不会回退 `ServiceSecret`；新接口还要求 `purpose=product-index:read`、`nbf/jti` 存在和有效期不超过 5 分钟，Agent 当前每次签发 1 分钟 Token。
- 专用客户端只允许精确的索引方法名，用服务 Token 替换出站 authorization，不叠加用户 Token；Mall 端再执行服务鉴权和 logic 身份检查。用户、管理员和支付服务均不能凭原 Token 读取此接口，Agent 索引 Token 不能调用商品写入、用户商品读取或订单/支付方法。
- 在线 `CheckProductCandidates` 继续由普通用户 JWT 授权，与后台索引客户端分离；核验失败不得用索引凭据重试或混入演示数据。候选校验只保护推荐响应/持久化，不是商品锁定、下单鉴权或历史结果的新鲜度承诺，详见 [M3.2c 策略](agent.md#213-候选证据与严格实时校验m32c)。
- `ScanProductIndex` 在建流时验签，不逐帧重新检查过期或撤销；原始流必须带不超过 30 秒的 deadline，拦截器在首次接收请求前检查，拒绝无界“只建流不发请求”。流式拦截器与一元拦截器共用方法策略，不能只保护 unary 后把 streaming 留空。Mall 在开发/测试模式注册的 reflection 流也经过该拦截器，按默认用户角色策略要求用户 JWT；它没有加入免鉴权或 Agent 索引服务策略。
- 只读共享密钥仍需安全传输和密钥管理；当前没有 mTLS、密钥轮换协议或 `jti` 一次性消费记录。索引接口并非用户鉴权缺口的全站修复，其他已列问题不因本次改动消失。

依据：[后台客户端鉴权](../services/rpc/agent/internal/rag/index_auth.go)、[分页索引逻辑](../services/rpc/mall/internal/logic/productindexservice/list_product_index_logic.go)、[快照逻辑](../services/rpc/mall/internal/logic/productindexservice/scan_product_index_logic.go)、[命名服务策略](../infra/interceptor/auth_interceptor.go)、[流式鉴权](../infra/interceptor/stream_auth_interceptor.go)。

## 6. 网络、运行时及错误返回边界

### 6.1 不能假定 RPC 天然不可达

当前 RPC YAML 模板监听 `0.0.0.0:10003–10007`，Compose 直接发布这些端口；HTTP 网关同样监听所有接口。实际是否能被外部连接取决于宿主机、容器网络和防火墙，但仓库配置本身并没有保证“只能从网关访问”。

仓库未提供这些业务服务的 Kubernetes NetworkPolicy / ServiceAccount RBAC 部署清单，也未在当前业务 RPC 装配中配置统一的 TLS/mTLS。DB、Redis、etcd 和 MQ 的网络/账号访问控制不能从应用 JWT 机制推导出来；Redis DB 编号、服务发现 key、消息消费者组均不是身份权限边界。

5 个 RPC 的数据库配置模板使用同一个数据库和 `root` 账号，连接参数包含 `sslmode=disable`，没有体现按服务分配的数据库最小权限。因此，代码层“由某个服务负责某张表”不等于数据库层已禁止其他服务访问该表。本文不据此推断实际部署环境的账号权限或防火墙规则。

RPC 在 dev/test 模式注册 gRPC reflection。Mall 和 Agent 已注册共享方法策略的 stream 鉴权，其 reflection 流按默认用户策略要求用户 JWT；只有明确配置的方法带 admission deadline（Agent 推荐流、Mall 索引流均为 ≤30 秒）。其它服务不能因注册了 unary 鉴权就推断流式/调试/健康服务都已受保护。网页推荐 SSE 的 v1 走 RecommendStream，旧请求保留 unary；不会因为 HTTP 已认证而省略 RPC 流式认证。

依据：[Compose](../docker-compose.yml)、各服务 `etc/config.yaml` 和 `main.go`、[etcd 配置中心](../infra/configcenter/configcenter.go)。

### 6.2 返回码与前端处理

- HTTP 认证中间件对缺失 Token、无效 Token、角色不足都直接返回 HTTP 401 文本，没有区分 401/403 的统一 JSON 权限响应。
- RPC 业务错误通过 [错误适配器](../infra/errors/http.go) 转为 HTTP 状态及 JSON；例如 `InvalidToken` 为 `401002`，`Unauthorized` 为 `401000`。不要把错误变量名理解成当前一定返回 403。
- 支付/秒杀订单的跨用户访问、Agent 会话未命中等场景使用 NotFound 类错误或明确的删除未命中结果，减少暴露他人资源是否存在。
- 秒杀一次性令牌失效 `401003` 不等于登录 JWT 失效，前端请求工具对该下单业务错误做了单独处理。
- RPC 调用日志已有方法、耗时、用户 ID、错误信息，但不是不可篡改的权限变更审计日志；`ValidateToken` 部分错误日志仍直接格式化原始 Token，尚未实现统一凭据脱敏。

依据：[错误定义](../infra/errors/errors.go)、[前端请求工具](../web-ui/src/api/request.ts)、[RPC 日志拦截器](../infra/interceptor/logging_interceptor.go)、[Token 校验日志](../services/rpc/auth/internal/logic/authservice/validate_token_logic.go)。

## 7. 待补齐事项（建议，不是现有能力）

优先级用于后续排期：P0 为上线前应优先闭合的越权边界，P1 为重要权限一致性/身份控制，P2 为治理能力。表中保留当前剩余缺口；Agent M1.3 已完成的用户文件隔离与工具白名单见第 4.5 节，不代表其他服务问题已解决。

| 编号 | 优先级 | 当前问题 | 建议补齐方式 |
| --- | --- | --- | --- |
| AUTH-01 | P0 | 用户管理 4 个 RPC 没有管理员检查，更新接口可修改角色 | RPC 方法级管理员鉴权；约束角色赋予范围，明确超级管理员保护策略 |
| MALL-01 | P0 | 普通订单方法信任请求 `UserId`，空 ID 可放宽查询范围 | 从认证上下文取用户；明确实现管理员跨用户分支，不以空 ID 代替授权 |
| MALL-02 | P0 | `UpdateOrderStatus` 没有 RPC 管理员检查，且可流转为已支付 | 增加方法级限制，并划分运营状态修改与支付确认权限 |
| MALL-03 | P1 | 幂等缓存键未按用户隔离，命中后不查归属 | 用户维度幂等键；缓存/数据库幂等命中都核对归属和请求一致性 |
| SECKILL-01 | P1 | 领令牌/下单使用请求用户；秒杀令牌只绑定 SKU | 使用认证用户；令牌绑定用户、活动、SKU，并原子核验消费 |
| AUTH-02 | P1 | 禁用状态未参与认证；旧 JWT 无统一撤销 | 统一账号状态校验，设计角色变更/禁用/退出后的失效机制 |
| AUTH-03 | P1 | JWT claim 中角色与数据库角色可能不一致 | 明确角色权威来源与缓存时效，避免 HTTP/RPC 判断不一致 |
| AUTH-04 | P1 | 部分认证日志包含原始 Token/验证码 | 敏感字段脱敏，补充日志回归测试；不要将凭据作为诊断文本 |
| SERVICE-01 | P1 | Agent 最小只读索引身份已完成本地实现；真实环境联调、密钥轮换及其他后台链路覆盖仍待补齐 | 在隔离环境验证部署配置与凭据失效，不以共享管理员用户 Token 替代服务身份 |
| NETWORK-01 | P1 | RPC 对宿主机发布，缺少仓库级网络隔离配置 | 限制开发端口绑定；部署时收敛 RPC 暴露范围、配置网络策略与传输安全 |
| AGENT-01 | P1 | MCP 缺少 OS/网络隔离及角色级授权，文件无累计容量配额 | 在已有用户文件隔离、精确白名单基础上补进程并发/资源配额、角色策略与部署隔离 |
| GOVERNANCE-01 | P2 | 缺少统一权限点、服务矩阵、角色变更审计及 MQ 身份控制 | 在具体业务需求确定后设计细粒度权限，并补充自动化权限回归矩阵 |

在这些缺口闭合前，建议只将当前配置用于受控开发环境；不能仅凭网关或前端的管理员判断认定已满足生产权限要求。

## 8. 已有测试与本次验证范围

| 现有测试位置 | 能证明的范围 | 不能据此推出的结论 |
| --- | --- | --- |
| [JWT 单测](../infra/auth/auth_test.go) | 签发、验签、过期及密码哈希等基础行为 | 账号禁用、撤销、管理员授权全部正确 |
| [通用鉴权测试](../infra/interceptor/auth_interceptor_test.go)、[服务 JWT 测试](../infra/serviceauth/service_auth_test.go) | 服务身份/受众/签名/过期及普通用户 Token 隔离 | 每个 RPC 都已配置正确的方法权限 |
| [Mall 方法矩阵](../services/rpc/mall/auth_test.go)、[在线核验权限](../services/rpc/mall/candidate_auth_test.go)、[用途 Token](../infra/serviceauth/scoped_token_test.go)、[后台客户端](../services/rpc/agent/internal/rag/index_auth_test.go)、[Loader 失败保护](../services/rpc/agent/internal/rag/loader_test.go) | 23 个 Mall RPC（22 unary＋1 server-stream）× 6 类身份的本地 gRPC 矩阵；真实核验 handler 另测用户/管理员允许、索引/支付/缺失身份拒绝；独立密钥、用途及索引失败保护 | 已访问真实 Mall/数据库、真实快照 MVCC/新鲜度已验收，或已有防重放/mTLS |
| [秒杀方法权限](../services/rpc/seckill/auth_test.go)、[秒杀订单归属](../services/rpc/seckill/internal/logic/seckillservice/get_order_logic_test.go) | 已列方法的角色边界、查订单绑定身份 | 领令牌与提交订单已绑定身份 |
| [支付校验](../services/rpc/payment/internal/logic/paymentservice/common_test.go)、[商城支付入口](../cmd/app/internal/logic/mall/payment_logic_test.go) | 部分跨用户/金额/通知检查 | 已真实调用支付宝、或完整支付链路无缺口 |
| [支付确认身份](../services/rpc/mall/internal/logic/orderservice/confirm_payment_auth_test.go) | logic 拒绝缺失/错误服务身份，正确身份进入参数校验 | 完成了真实数据库支付确认事务测试 |
| [Agent 身份提取与历史脱敏](../services/rpc/agent/internal/logic/recommendservice/common_test.go)、[文件隔离](../services/rpc/agent/internal/filetools/security_linux_test.go)、[MCP 策略](../services/rpc/agent/internal/agent/recommend/llm/mcp_test.go) | 可信身份、用户私有文件、保存授权、路径竞态、工具白名单和元数据脱敏 | MCP 已被 OS/网络沙箱化、外部服务端确实只读或真实部署已验收 |
| [Agent 流式内存 RPC](../services/rpc/agent/internal/logic/recommendservice/recommend_stream_transport_test.go)、[流式故障注入](../services/rpc/agent/internal/logic/recommendservice/recommend_stream_failure_test.go)、[流式 Token 透传](../infra/interceptor/stream_client_interceptor_test.go) | 无/过期/伪造 Token、错误角色、服务凭据、无界 deadline 被拒绝；同名会话用户隔离、取消/慢 Fake Send、原子保存后发送及提交后重放 | 已接入网页或真实模型增量、经过代理断连/真实多进程测试，或网络投递/工具副作用恰好一次 |
| [Eino 流式集成](../services/rpc/agent/internal/logic/recommendservice/recommend_stream_model_test.go)、[事件预算/背压](../services/rpc/agent/internal/logic/recommendservice/stream_progress_test.go)、[模型预算/工具事件](../services/rpc/agent/internal/agent/recommend/llm/stream_test.go) | Fake Model 实际 Pipe 增量早于生成完成；晚到工具调用、隐藏字段隔离、工具关联、协议/资源拒绝、发送失败与取消、增量后核验/保存失败无 final、不整轮兜底、已完成重放不重跑 | 真实模型兼容性/费用/时延、网页端到端、第三方依赖可被强杀，或临时自然语言已通过事实校验 |
| [网关 HTTP/RPC](../cmd/app/internal/logic/agent/agent_stream_http_test.go)、[网关协议](../cmd/app/internal/logic/agent/agent_stream_protocol_test.go)、[浏览器协议](../web-ui/tests/recommendation-stream.spec.ts)、[页面交互用例](../web-ui/tests/recommendation.spec.ts) | 本地 HTTP + 内存 gRPC 走生产 Token 验签，验证先增量后终态、取消/写入失败、不自动重跑、身份/序号与兼容边界；协议与页面用例的实际运行结果见最新执行记录 | 真实部署/代理/供应商验收、全链路日志均已脱敏，或尚未运行的交互用例已经通过 |
| [保留库真实浏览器](../web-ui/tests-local/agent-retained.spec.ts)、[显式配置保护](../web-ui/tests/agent-local-config.spec.ts) | 获准本机库的真实 Auth 登录、匿名拒绝、两个普通账号的双向历史隔离、页面 SSE 写入/刷新/重放；配置拒绝规则另用合成输入验证 | loopback 白名单自动保证上游配置安全、真实模型/商城/交易/管理权限或部署代理验收、日志已全面脱敏 |
| [分段预算/记录器](../services/rpc/agent/internal/runtrace/recorder_test.go)、[Service 预算](../services/rpc/agent/internal/agent/recommend/service_budget_test.go)、[模型用量](../services/rpc/agent/internal/agent/recommend/llm/stream_usage_test.go)、[RPC 汇总](../services/rpc/agent/internal/logic/recommendservice/recommend_stream_trace_test.go) | 短期限预留、持锁值保留、迟到结果拒绝、提交后取消、累计 Usage 去重与 unknown、并发隔离/冻结、汇总日志无敏感正文、重放不产生新用量 | 真实账单/供应商完成标记、页面首 Token 时延、第三方任务被强杀或真实 DB/代理故障已验收 |
| [Redis 租约故障](../services/rpc/agent/internal/memory/redis_fault_test.go)、[共享存储 RPC 矩阵](../services/rpc/agent/internal/logic/recommendservice/recommend_fault_matrix_test.go)、[两级缓存故障](../services/rpc/agent/internal/memory/tiered_fault_test.go) | 两个独立 Service 的同轮重放、跨用户/会话隔离与删除顺序；过期/替换锁不得写入或误删后继锁，WATCH 连接丢失不裸重试；缓存失效失败不掩盖 durable 提交边界 | 真实多进程/数据库、Redis 切主/Cluster、真实滚动回滚、缓存替身代表 PostgreSQL 事务，或所有写实例已升级 |
| [真实存储多进程矩阵](../services/rpc/agent/internal/storageacceptance/matrix_test.go)、[重启/恢复重放](../services/rpc/agent/internal/storageacceptance/recovery_test.go)、[运行器保护测试](../scripts/test_agent_storage_acceptance.py) | 用户授权的新建 PG/Redis 单节点上验证多进程存储边界、异常停止后重放和新 PG 数据目录恢复；只持有所建进程句柄，忽略原部署 DSN，实际版本/运行记录见 M6.2b | 已收紧现有数据库角色、完成生产权限/ACL 恢复、外部模型/部署流式联合验收、Cluster/切主/掉电或实际版本回滚 |

2026-09-16 文档初次核对执行并通过以下现有测试，当时未修改业务代码，也未运行真实跨服务越权请求或生产数据操作；后续 M1.3 验证见 [Agent 执行记录](agent.md#14-执行记录)：

```bash
go test ./infra/auth ./infra/interceptor ./infra/serviceauth \
  ./services/rpc/seckill \
  ./services/rpc/seckill/internal/logic/seckillservice \
  ./services/rpc/payment/internal/logic/paymentservice \
  ./services/rpc/agent/internal/logic/recommendservice \
  ./services/rpc/agent/internal/filetools \
  ./cmd/app/internal/logic/mall

go test ./services/rpc/mall/internal/logic/orderservice \
  -run '^TestConfirmPayment(RejectsMissingServiceIdentity|RejectsWrongServiceIdentity|AcceptsPaymentIdentity)$' \
  -count=1
```

同时核对了全部 54 个 RPC 方法、64 条 HTTP 路由（App 29 条、Admin 35 条）和本文 74 个本地源码/文档链接。HTTP 路径以 `.api` 与已生成的 `routes.go` 双向核对；该清单检查不等价于逐接口端到端权限测试。

2026-09-17 M3.1 新增 1 个 Mall 索引 RPC（HTTP 路由未变），补跑 Mall 全部 21 个 RPC 的 6 类身份矩阵及支付/Agent 回归；上面的 54/74 为初次核对时的历史数量。最新验证范围、跳过项和真实环境边界见 [M3.1 执行记录](agent.md#14-执行记录)。

2026-09-18 M3.2b2 再增 1 个只读快照流 RPC，矩阵扩为 22 个方法；新增流式建连凭据、deadline/慢接收者及源事务/终帧/EOF 测试。HTTP 路由、用户及支付方法权限不变，真实数据库测试仍须显式隔离环境，见 [最新执行记录](agent.md#14-执行记录)。

2026-09-18 M3.2c 新增普通用户身份的 `CheckProductCandidates`，矩阵扩为 23 个方法；在线严格核验与后台索引服务身份保持分离，现有 HTTP/支付权限不变。新增数据库新鲜度集成测试为显式 DSN 门控，本次仅编译并跳过。

2026-09-18 M4.2c 扩展同一核验 RPC 的显式分类读取，不新增 Mall 方法或扩大角色权限。生产 handler 的内存 RPC 测试对两种请求均覆盖用户/管理员允许与缺失/索引/支付凭据拒绝；Agent 侧内存 gRPC 补测用户 JWT 透传、规划/执行/历史重放、他人会话隔离及旧 Mall 拒绝。分类迁移、实际数据库权限与真实数据尚未验收，没有自动建表、赋权或回填。

后续修改 `.api`、`.proto`、方法权限集合、用户状态/角色逻辑、资源归属校验或服务调用凭据时，应同步更新本文及对应回归测试。不要只更新前端菜单或 API 注释。
