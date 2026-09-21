# AI 协作约定

本文件供编码助手快速定位规则，不重复维护服务拓扑、环境现状或阶段日志。人类开发者先看 [README](README.md) 和[文档导航](docs/README.md)，提交约定以 [开发规范](docs/CONTRIBUTION.md) 为准。

## 先定位，再修改

| 任务 | 先读 |
| --- | --- |
| 理解代码职责 | [架构与代码地图](docs/architecture.md) |
| 判断完成范围 | [项目状态](docs/status.md)，不要以旧执行日志中的“下一步”为当前任务 |
| 修改 Agent | [Agent 指南](docs/AGENT.md)、相关服务配置与测试 |
| 修改权限、用户或订单 | [权限与安全](docs/access-control.md)，确认已有缺口而非假定网关已保护 RPC |
| 修改环境或部署 | [配置指南](docs/SECRETS.md)、[本地数据源](docs/local-data.md)、[VPS 运维](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops#部署流程) |
| 运行测试或排查门禁 | [CI 指南](docs/CONTRIBUTION.md#go-检查与测试环境)、[前端指南](docs/frontend.md) |

以当前代码、接口定义和配置模板为事实来源；历史快照仅提供设计与证据，不代表当前运行状态或新的操作授权。

## 编码与生成规则

- 网关不直接访问业务数据库，数据操作通过对应 RPC；资源归属与方法权限必须在 RPC 内独立落实。
- HTTP 契约从 `cmd/<app>/desc/**/*.api` 修改，RPC 契约从 `services/rpc/<service>/proto/` 修改。业务实现主要位于 `internal/logic/`，依赖组装位于 `internal/svc/`。
- 不手改生成的 `pb/`、`client/`、RPC server、网关 handler、`types.go`、`routes.go`。需要时修改定义或 `tpls/` 后生成，并检查差异。
- `make api-all` 会同时重新生成代码和被 Git 忽略的 Swagger JSON；不是无副作用的文档预览命令。
- 新增 Eino 工具在 [tools.go](services/rpc/agent/internal/agent/recommend/llm/tools.go) 定义入参、handler 和注册逻辑，使用 `jsonschema` tag 与 `utils.InferTool`，并补权限、参数及失败路径测试。
- 金额保持整数分；并发、幂等与权限改动必须覆盖拒绝路径，不能只验证成功请求。

## 错误与日志

统一错误来自 [infra/errors](infra/errors/README.md)，`AppError.Error()` 保留 `code:msgId` 协议。

- RPC logic 返回明确的业务错误；API logic 收到 RPC 错误后保留原错误，不重新包装成 `Internal` / `Database` 丢失业务码。
- API 本地校验分支直接返回相应本地错误；不要把空 RPC 响应等异常当作成功。
- error 返回路径记录必要的 `logx` 操作上下文，但原始错误、Token、验证码、密码、完整 DSN、提示词和工具正文可能敏感，须先脱敏或使用安全摘要。
- 不因 Agent 局部日志测试通过就声称其他服务日志已全面脱敏。

## Agent 不变量

- 已配置 Mall 不混用 Mock；已配置数据库失败不静默降级成丢历史的临时存储。未配置可选依赖与配置错误是不同情况。
- `conversation_id` 标识会话，`turn_id` 用于同轮重放；跨用户隔离、请求冲突和原子保存必须保留。
- `stream_version: 1` 使用真实 RPC/SSE；成功要求 final、成功 done 和正常 EOF。流中失败不能自动重跑 unary 或整轮模型。
- 后台索引使用 Mall 完整一致快照，完成帧与正常 EOF 后才允许发布/清理；先升级 Mall，不回退旧实时分页协议。
- 索引模型/端点/维度不匹配不得自动删表、重建或伪造绑定。同步使用专用 PostgreSQL 会话连接，不支持 transaction pooling；断连后不能换连接续写旧轮次，升级前停旧同步器。
- 配置 Mall 后，最终返回与保存前必须核验当前候选事实；索引同步成功不是库存锁定或历史结果新鲜度证明。
- 文件工具和 MCP 默认关闭；保留当前用户隔离、显式保存授权、精确工具白名单及进程回收，不把只读声明当作 OS / 网络沙箱。
- `deepseek-flash` 需 `LLM_THINKING=disabled`，其他模型留空。不得只换模型名而忽略二进制、配置、回退和调用费用边界。

## 环境、测试与授权

- 不覆盖已有 `.env`，不把真实密钥写入仓库、输出或报告。本地优先复用已核对的 Docker 数据源，不从模板推断现有密码/端口。
- `make dev` 会加载 `.env`、启动基础设施和后端并清理开发端口；启动 Agent 还可能进行索引同步。诊断或文档任务不默认授权这些运行操作。
- `make test` 不加载 `.env`，也不准备依赖。数据库集成测试只用显式可丢弃环境，不能连接业务库、生产库或保留演示库；部分测试未配置时会跳过，必须如实报告。
- 真实模型 / Embedding 调用、数据迁移、写演示记录、发版、重启与清理分别核对授权范围。旧日志中的调用次数/费用授权不自动续期。
- 离线评测不改写原始报告或答案，不移动已知保留集掩盖失败，不代填人工复核身份；终态门禁不等于业务质量 100%。
- 区分本机替身、真实单机依赖、浏览器和生产证据；本地测试成功不代表目标 K3s 或供应商已验收。

## 交付与文档

保留用户已有改动，只提交明确范围；遵守项目提交类型，不自动推送、发布或将不相关内容混入提交。

生产镜像、GitOps、Argo CD 声明与运维记录位于独立的 [BudgetMatch-Sim-Gitops](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops)；本仓库维护业务代码、代码 CI 和本地开发，生产部署仅由 Argo CD 手动 Sync。

更新使用方法时改对应指南；整体完成范围只更新 [status.md](docs/status.md)；长篇执行记录放[历史归档](docs/archive/README.md)。移动文档后校验相对链接与章节锚点，不再往 README 和本文件重复追加开发日志。
