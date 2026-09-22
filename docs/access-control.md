# 权限与安全边界

[文档导航](README.md) · [项目状态](status.md) · [完整接口核对与测试快照](archive/access-control-2026-09.md)

本文按仓库实现描述权限，不是完整安全审计结论。Kubernetes / Headlamp 的 RBAC 与业务权限互相独立；管理界面的安装不会补齐下面的 RPC 越权问题。

## 先看结论

项目采用 HTTP 网关鉴权、RPC 方法鉴权与部分资源归属校验。已有用户/管理员区分，但未实现细粒度权限点、租户或商户隔离。Auth 用户管理、Mall 订单归属/状态、订单幂等隔离与秒杀用户绑定已在 RPC 层补齐校验；部署与会话撤销的剩余限制见下文。

## 身份与通用规则

| 身份 | 实际判定 |
| --- | --- |
| 公开 | 不需要用户 JWT；仍可能需要密码、验证码或支付签名 |
| 用户 | `role` 在 1–199，包含管理员；注册默认 100 |
| 管理员 | `role` 在 1–99；枚举 1 为超级管理员、2 为管理员；只有超级管理员可以修改/删除管理员账号或改变用户角色 |
| 服务专用 | 精确方法策略要求独立服务 JWT，不接受普通用户/管理员 JWT 替代 |

用户 JWT 默认有效期 24 小时，签发使用 HS256。通用验证仅接受 HS256，要求非空身份、整数角色及有效 `exp`；尚未要求用户 Token 的 `iss` / `aud` / `jti`。目前没有服务端登出撤销、Token 黑名单、Token 版本或刷新令牌接口。

账号 `StatusNormal=1`、`StatusDisabled=2`。三种登录入口、`ValidateToken` 和 auth-rpc 拦截器只接受启用账号；禁用、删除和角色不匹配的旧 JWT 在后续请求中被拒绝。

### HTTP 与 RPC 使用同一账号状态来源

HTTP 网关调用 `auth-rpc.ValidateToken`，读取数据库中的当前用户和角色，再透传原 JWT。Mall、Seckill、Payment、Agent 的启动入口为一元/流式拦截器安装同一个 Auth RPC 核验器；用户请求先验签，再核验数据库状态与角色是否仍匹配。Auth 不可用时拒绝请求，不回退到仅凭 JWT 放行。服务专用 JWT 仍走独立策略。

每次新请求/建流检查当前状态，已进入业务处理的请求和已建立的流不会被强制撤回。目前没有永久撤销记录：账号重新启用或角色恢复后，尚未过期的对应旧 JWT 可能再次有效。

通用 RPC 策略优先级为：服务专用方法 → 公开方法 → 用户 JWT → 管理员方法或默认用户范围。**未加入管理员集合的新方法默认只要求用户，不是默认拒绝。**

依据：[角色定义](../infra/role/role.go)、[用户 JWT](../infra/auth/auth.go)、[通用 RPC 拦截器](../infra/interceptor/auth_interceptor.go)、[auth-rpc 拦截器](../services/rpc/auth/internal/interceptor/auth_interceptor.go)。

## 网关与服务权限速查

| 服务 | 入口控制 | 数据范围与注意事项 |
| --- | --- | --- |
| App HTTP | 健康检查、注册/登录/验证码、支付宝通知公开；其余已注册业务接口要求用户 | 网关为订单/秒杀注入当前用户 ID；商品不是游客接口 |
| Admin HTTP | 健康检查公开，其余已注册业务接口要求数据库角色 1–99 | 全局管理，无运营/财务/商户分权；角色赋予与管理员账号修改由 RPC 限制为超级管理员 |
| auth-rpc | 认证方法白名单，其余要求数据库角色 1–199 | `GetUserInfo` 固定本人；其他 4 个用户管理方法要求管理员，logic 同样校验 |
| seckill-rpc | 活动/SKU 写入及 `GetSku` 要求管理员；活动与 SKU 列表要求用户 | 查询订单、领令牌、下单均绑定认证用户；传入其他用户 ID 会被拒绝 |
| mall-rpc | 商品写入和 Outbox 管理要求管理员；支付确认/后台索引要求不同服务身份 | 创建/取消订单限定本人；查询/列表仅管理员可跨用户；状态修改要求管理员且不能标记已支付 |
| payment-rpc | 创建/查询要求用户；通知免用户 JWT | 支付流水绑定 JWT 用户，核对订单归属与真实金额；通知仍须验签与业务检查 |
| agent-rpc | 7 个推荐/需求/会话 RPC 均要求用户 | 会话按认证用户隔离，无管理员跨用户特权；后台索引使用独立身份 |

App 商品/活动查询仍会透传部分状态等筛选条件，不能描述为“服务端始终只向用户返回上架资源”。前端隐藏菜单、刷新管理员角色和表单限制都只是交互控制，不是后端授权。

秒杀资格令牌是另一种凭据：TTL 60 秒，绑定用户、活动与 SKU，通过 Lua 原子匹配并删除。错误用户/活动/SKU 不能消费令牌；匹配成功后只能消费一次。升级前仅绑定 SKU 的旧令牌被拒绝，需重新领取。

权威接口入口：[App API](../cmd/app/desc/app.api)、[Admin API](../cmd/admin/desc/admin.api)、[Mall 方法策略](../services/rpc/mall/internal/config/auth.go)、[秒杀方法策略](../services/rpc/seckill/auth.go)。逐接口矩阵保留在[历史核对记录](archive/access-control-2026-09.md)。

## 服务身份与工具边界

| 链路 | 使用的身份 | 关键限制 |
| --- | --- | --- |
| 网关 → 业务 RPC | 原始用户 JWT | 不是网关自身服务身份；下游仍须校验数据归属 |
| Agent 在线检索 / 候选核验 → Mall | 当前用户 JWT | 核验失败不能改用后台索引密钥绕过；候选最多 32 个 |
| Agent 后台索引 → Mall | `AGENT_MALL_INDEX_SECRET` 签发的用途 JWT | 只允许精确索引方法，`purpose=product-index:read`；当前 TTL 1 分钟、允许有效期至多 5 分钟 |
| Payment → Mall `ConfirmPayment` | `PAYMENT_MALL_SERVICE_SECRET` 签发的服务 JWT | 固定 HS256、调用方 payment-rpc、受众 mall-rpc；当前 TTL 1 分钟，logic 再检查服务身份 |
| 业务服务 → RocketMQ | 现有 Topic / Tag / 消息载荷约定 | 不是 JWT 链路；仓库客户端未接入 MQ 凭据或消息签名授权 |

索引密钥至少 32 字节，Agent / Mall 一致且独立于用户/支付密钥；命名密钥缺失不会回退支付密钥。支付密钥也应与用户 JWT 分离，但代码未统一检查两者不相等。短期 JWT 的 `jti` 尚无一次性消费记录，不能称为不可重放凭据；订单幂等是另一层机制。这些共享密钥机制不等于 mTLS。

Agent 与 Mall 已有流式鉴权，推荐流和索引流在首次接收前要求不超过 30 秒的 deadline；JWT 在建流时检查，不逐帧重新校验。Mall、Agent、Seckill、Payment 的 dev/test reflection 流也走用户策略；Auth 的 reflection 尚未安装流式拦截器，仅在 dev/test 注册。

文件工具与 MCP 默认关闭。文件按认证用户隔离，写入另需开关和本轮首行 `/save <相对路径>`，只创建不覆盖；MCP 要求可信已安装程序、精确工具白名单和只读声明，并限制子进程环境及生存期。MCP **没有 OS / 网络沙箱**，只读声明不等于安全证明。详见 [Agent 工具说明](AGENT.md#文件与-mcp-工具)。

依据：[服务 JWT](../infra/serviceauth/service_auth.go)、[索引身份](../services/rpc/agent/internal/rag/index_auth.go)、[流式拦截器](../infra/interceptor/stream_auth_interceptor.go)。

## 网络与错误返回

- RPC 模板监听 `0.0.0.0:10003–10007`，Compose 发布对应宿主端口；是否公网可达还取决于网络和防火墙，不能假定只有网关能访问。
- 当前 Compose 的数据依赖端口有回环绑定，不等于 RPC 已同样收敛。仓库尚未提供业务 NetworkPolicy 或统一 RPC TLS/mTLS；生产外部数据库也有未启用 TLS 的边界，见 [VPS 运维](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops#部署流程)。
- 数据库连接现由环境变量注入。模板使用本地开发账号与 `sslmode=disable`，不是按服务最小权限方案；不能据此断言生产账号权限。Redis DB 编号、etcd key、MQ 消费组都不是安全隔离边界。
- HTTP 鉴权中间件对缺 Token、无效 Token、角色不足均返回 401 文本，尚未统一区分 401/403 JSON。业务错误另由[错误库](../infra/errors/README.md)适配。
- 秒杀业务错误 `401003` 表示资格令牌失效，不代表登录 JWT 失效。Agent 会话、支付/秒杀订单的部分跨用户访问以 NotFound 或删除未命中处理，减少资源存在性泄露。
- 已移除 `ValidateToken` 的原始 Token 错误日志和登录参数校验中的验证码日志；这不代表全项目日志审计完成，已有 RPC 日志也不是不可篡改审计记录。

## 本次修复与升级要求

| 编号 | 已实现保护 |
| --- | --- |
| AUTH-01 | 用户管理 RPC 和 logic 双层管理员校验；只有超级管理员可改变角色或修改/删除管理员账号 |
| MALL-01 / MALL-02 | 资源归属来自认证上下文；管理员查询范围显式放宽；运营状态接口不能转为已支付，支付确认保持服务专用 |
| MALL-03 | Redis 与数据库幂等键加入用户维度；重放核验归属、SKU、数量、备注并返回当前状态；旧订单仅在归属和请求匹配时兼容重放 |
| SECKILL-01 | 用户 ID 来自认证上下文；资格令牌原子匹配用户、活动、SKU 并一次性消费 |
| AUTH-02 / AUTH-03 | 登录与请求核验禁用/删除账号，HTTP 与业务 RPC 均核验当前数据库角色；角色改变后须重新登录 |
| AUTH-04 | 删除已知原始 JWT / 验证码日志输出 |

Mall、Seckill、Payment、Agent 的配置必须包含 `AuthRpc`（仓库 YAML 已补齐，默认通过 etcd 的 `auth.rpc` 发现服务）。外部部署配置需要同步添加该段，允许这些服务访问 Auth；缺少配置时不支持启动降级。每个用户 RPC 新增一次 Auth 请求，部署时需验证容量和超时。服务 JWT 支付确认/后台索引不新增此依赖。

订单新幂等键为用户与客户端键的摘要，继续使用现有唯一索引，无数据库结构迁移；停止旧版本订单写入后切换，避免新旧写入规则并存。发布秒杀变更时，60 秒内的旧资格令牌需重新领取。用户 JWT 仍兼容现有 HS256 / `user_id` / 整数 `role` / `exp` 格式。

## 待修复风险

以下是未完成工作，不是已实现的保护。P0 优先闭合越权边界；P1 为重要身份和部署控制；P2 为治理能力。

| 编号 / 优先级 | 当前缺口 | 后续修复方向 |
| --- | --- | --- |
| AUTH-02 / P1（剩余） | 已实现当前状态核验，尚无永久 Token 撤销/登出失效记录，已建立流不重新验证 | 增加会话版本或撤销表与明确失效时效 |
| SERVICE-01 / P1 | 服务身份尚未覆盖所有后台链路，部署联调和轮换待补 | 独立最小权限身份、部署验证、失效和轮换流程 |
| NETWORK-01 / P1 | RPC 暴露、网络隔离及传输安全不足 | 收敛监听/发布范围、网络策略、TLS / 私网连接 |
| AGENT-01 / P1 | MCP 无 OS/网络隔离、角色级授权和进程并发配额；文件无累计配额 | 资源限制、授权策略与部署隔离 |
| GOVERNANCE-01 / P2 | 无统一权限点、角色变更审计及 MQ 身份控制 | 按业务需求建立权限矩阵与可验证审计 |

## 修改与回归要求

修改 `.api`、`.proto`、方法集合、角色/状态、资源归属或服务凭据时，同步更新本页和对应测试；不能只改页面或接口注释。

现有测试入口包括 [通用拦截器](../infra/interceptor/auth_interceptor_test.go)、[Mall 方法矩阵](../services/rpc/mall/auth_test.go)、[秒杀订单归属](../services/rpc/seckill/internal/logic/seckillservice/get_order_logic_test.go)、[支付校验](../services/rpc/payment/internal/logic/paymentservice/common_test.go)和 [Agent 流式鉴权](../services/rpc/agent/internal/logic/recommendservice/recommend_stream_transport_test.go)。新增回归包括 [Auth 真实 RPC 权限测试](../services/rpc/auth/access_control_test.go)、[商城归属与幂等](../services/rpc/mall/internal/logic/orderservice/access_control_test.go)、[秒杀令牌原子绑定](../services/rpc/seckill/internal/stock/stock_manager_test.go)和 [Auth 故障拒绝策略](../infra/authclient/validator_test.go)。测试通过仅说明已覆盖的断言，不表示剩余待办已修复。

详细历史测试范围见[核对归档](archive/access-control-2026-09.md)，数据库测试隔离规则见 [CI 指南](CONTRIBUTION.md#go-检查与测试环境)。不要为了验证权限对生产数据执行越权写入。
