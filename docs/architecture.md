# 架构与代码地图

[文档导航](README.md) · [本地启动](../README.md#本地快速开始) · [权限与安全](access-control.md)

## 服务分层

浏览器 → App / Admin HTTP 网关 → gRPC 业务服务 → 数据库、缓存和消息队列。

网关处理请求、身份上下文和 RPC 编排，业务数据操作通过 RPC 完成；网关不直接访问业务数据库。RPC 服务负责业务规则、数据归属和持久化，不能只依赖上游页面或网关限制。

| 组件 | 默认服务端口 | 职责 |
| --- | --- | --- |
| `web-ui` | 5173（开发）/ 8080（Compose） | 用户端、管理端与 SSE 消费 |
| `cmd/app` | 10002 | 客户端 HTTP API |
| `cmd/admin` | 10001 | 管理端 HTTP API |
| `services/rpc/auth` | 10003 | 登录、注册、JWT 校验、用户管理 |
| `services/rpc/seckill` | 10004 | 活动、资格令牌、秒杀排队与订单 |
| `services/rpc/mall` | 10005 | 商品、SKU、商城订单、Outbox、推荐候选事实 |
| `services/rpc/agent` | 10006 | 推荐、会话、约束、检索、需求执行与流式响应 |
| `services/rpc/payment` | 10007 | 支付流水、支付宝通知与订单确认 |

这些是服务监听端口，不代表都应对公网开放。实际暴露方式由 Compose / K3s 配置决定；当前 RPC 安全缺口见[权限文档](access-control.md#待修复风险)。

## 数据与基础设施

| 依赖 | 用途 | 地址规则 |
| --- | --- | --- |
| PostgreSQL | 用户、商品、订单、支付、Agent 会话和可选向量索引 | 容器内 5432；宿主机映射按环境核对 |
| Redis | 缓存、限流、幂等/并发控制、秒杀状态等 | 容器内 6379；不同 DB 编号不是权限隔离 |
| etcd | 服务发现和动态配置 | 容器内 2379；`make dev` 默认映射 22379 |
| RocketMQ | 秒杀与订单异步消息 | NameServer 容器内 9876，`make dev` 默认映射 19876；Broker 10911 / VIP 10909 |

容器间使用 `postgres:5432`、`redis:6379`、`etcd:2379` 等服务名。宿主机进程使用映射端口；`127.0.0.1` 在容器或 Pod 内只指向它自身。模板、本机复用数据和生产外部数据库的区别见[本地数据源](local-data.md)与 [VPS 运维](deployment-vps.md)。

Agent 的数据链路为：Mall 一致快照 → Embedding / 索引 → 有界检索与组合 → Mall 当前事实核验 → 保存和返回。索引同步、用户鉴权和最终事实核验是不同层次的保证，详见 [Agent 指南](agent.md)。

## 代码地图

| 路径 | 内容 |
| --- | --- |
| [cmd/app/desc](../cmd/app/desc/) / [cmd/admin/desc](../cmd/admin/desc/) | HTTP `.api` 定义；接口修改从这里开始 |
| `cmd/<app>/internal/logic/` | 网关业务编排，手写代码 |
| `services/rpc/<service>/proto/` | RPC 接口定义 |
| `services/rpc/<service>/internal/logic/` | RPC 业务实现 |
| `services/rpc/<service>/internal/svc/` | 依赖注入与启动初始化 |
| `services/rpc/<service>/model/` | 数据访问与持久化 |
| [infra](../infra/) | JWT、鉴权拦截器、错误库、数据库等公共设施 |
| [web-ui](../web-ui/) | 前端与 Playwright 测试 |
| [scripts](../scripts/) | 开发、CI、迁移和部署脚本 |
| [deploy](../deploy/) | 环境声明、镜像与 GitOps 配置 |
| [tests/cicd](../tests/cicd/) | CI/CD 工具回归测试 |
| [tpls](../tpls/) | goctl 模板 |

`pb/`、`client/`、网关 `handler/`、`types.go`、`routes.go` 等为生成代码，优先修改接口定义或模板后再生成，不直接手改。`make api-all` 同时更新生成代码和被 Git 忽略的 Swagger 文件，执行后须检查差异。

工具链以 [go.mod](../go.mod)、[前端 package.json](../web-ui/package.json)和构建文件为准；版本同步、错误处理、分层及提交规则见[开发规范](../Contributors.md)。
