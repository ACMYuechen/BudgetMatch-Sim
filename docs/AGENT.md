# Agent 指南

[文档导航](README.md) · [项目状态](status.md) · [详细设计与验收归档](archive/agent-development.md)

本页说明当前代码怎么用、哪些保证必须保留。M1–M6 的方案比较、逐次测试结果和历史操作统一放在归档，不作为新的调用或发布授权。

## 能力与入口

Agent 支持规则 / Eino ReAct 推荐、多轮约束继承、持久会话、幂等重放、商品向量检索与实时核验、受控需求执行，以及版本化 RPC/SSE 流式响应。它生成建议，**不预占库存、不自动下单**。

| 入口 | 用途 |
| --- | --- |
| [服务配置](../services/rpc/agent/etc/config.yaml) | 模型、存储、检索、工具、执行开关与默认值 |
| [HTTP 定义](../cmd/app/desc/agent/agent.api) | 请求、响应、会话与 SSE 契约 |
| [RPC 定义](../services/rpc/agent/proto/agent.proto) | Agent RPC 契约 |
| [推荐编排](../services/rpc/agent/internal/agent/recommend/) | 约束合并、规则 / 模型执行 |
| [检索工具](../services/rpc/agent/internal/tools/) / [索引同步](../services/rpc/agent/internal/rag/) | Mall 候选、向量与混合召回、在线事实核验 |
| [会话存储](../services/rpc/agent/internal/memory/) / [运行统计](../services/rpc/agent/internal/runtrace/) | 并发、幂等、持久化与用量记录 |
| [需求模型](../services/rpc/agent/internal/demand/) / [Beam 选择](../services/rpc/agent/internal/recommend/beam/) | 结构化需求、覆盖约束与有界组合 |

## 选择运行模式

| 模式 | 配置条件 | 行为与边界 |
| --- | --- | --- |
| 规则推荐 | `LLM_PROVIDER` 留空或 `noop` | 不调用 LLM；商品仍来自已配置的 Mall |
| 模型推荐 | 完整 `LLM_*` 配置 | Eino ReAct 使用 `search_products`、`select_bundle` 等受控工具 |
| 关键词检索 | 不启用 Embedding | 从 Mall 检索；不是向量检索失败后的 Mock 数据源 |
| RAG | Mall、PostgreSQL/pgvector、Embedding、独立索引密钥齐备 | 后台同步和查询会调用 Embedding；配置不合法时拒绝启动 |
| Mock 演示 | 明确移除整个 `MallRpc` 配置块 | 仅演示商品，不对应可购买的真实 SKU |

“没有配置可选能力”与“已配置但连接失败”不同。已配置 PostgreSQL 或 Mall 出错，不能静默改用临时记忆或 Mock 并冒充成功；模型非流式路径有受控规则回退，但已开始的版本化流不能失败后自动重跑 unary。

### 模型与 Embedding

常规环境变量见[配置指南](SECRETS.md)。使用 `deepseek-flash` 时，必须同时设置 `LLM_THINKING=disabled`；换用其他模型时留空，不自动转换旧模型名。部署时须将支持该字段的新二进制与配置配套核对。

硅基流动 BGE-M3 的应用配置为：

```dotenv
EMBEDDING_PROVIDER=openai
EMBEDDING_MODEL=BAAI/bge-m3
EMBEDDING_BASE_URL=https://api.siliconflow.cn/v1
EMBEDDING_DIMENSIONS=1024
```

另外通过私密配置提供 `EMBEDDING_API_KEY` 和 Agent / Mall 一致的 `AGENT_MALL_INDEX_SECRET`。索引密钥至少 32 字节，独立于用户 JWT 和支付服务密钥；Embedding 与 LLM 的接口和密钥分别维护。

上述 BGE-M3 接线不向远端发送 `dimensions` 参数，本地索引仍校验 1024 维。不要沿用模板的 1536 维或擅自替换模型名。索引绑定 provider、model、endpoint、dimensions；不匹配或旧非空索引缺少绑定时拒绝使用，**不会自动删表重建**。变更模型需要单独迁移和回退方案；额度、价格与实际账单不由此配置保证。

## 接口与会话

以下 HTTP 接口均要求用户 JWT，用户身份从已验证上下文取得，不接受请求自行指定其他用户。

| 方法与路径 | 用途 |
| --- | --- |
| `POST /api/agent/recommend` | 一次推荐 |
| `POST /api/agent/recommend/stream` | SSE；`stream_version: 1` 启用真实 RPC 流 |
| `POST /api/agent/intent/plan` | 形成需求计划，不检索或调用模型 |
| `POST /api/agent/intent/execute` | 执行已确认的最新计划，需要显式开启执行模式 |
| `GET /api/agent/conversations` | 当前用户的会话分页 |
| `GET /api/agent/conversations/:conversation_id/turns` | 当前用户的会话轮次 |
| `DELETE /api/agent/conversations/:conversation_id` | 删除自己的会话 |

同步请求示例（占位值需自行替换，不把真实 Token 写入文档或共享日志）：

```http
POST /api/agent/recommend
Authorization: Bearer <登录返回的 Token>
Content-Type: application/json

{"query":"预算500元搭配数码配件","budget_cents":50000,"max_items":3,"turn_id":"<本轮 UUID>"}
```

- 金额统一使用整数**分**：500 元对应 `50000`；`query` 必填且不超过 2000 字符，`max_items` 不超过 10。完整边界以 API / RPC 校验为准。
- 继续对话带上响应中的 `conversation_id`，新一轮使用新的 `turn_id`。同用户、会话、轮次和相同有效请求用于重放；同一轮次改换请求应返回冲突。
- PostgreSQL 保存完整轮次与结构化状态；Redis 为一级缓存，或在未配置数据库时提供短期记忆；均未配置时才使用进程内记忆。
- 默认上下文读取最多 20 条历史消息（10 轮），近似 Token 上限 8000；Redis / 内存 TTL 默认 24 小时，不代表 PostgreSQL 历史自动过期。
- 会话按用户隔离，管理员没有跨用户读取 Agent 会话的默认特权。历史重放保留当时结果，不承诺当前价格、库存仍相同。

## 流式完成语义

`stream_version` 省略或为 `0` 时保留旧版 unary 阶段 SSE；`1` 才使用真实 Agent RPC / Eino 流。两种协议不能混同。

新协议包含执行/会话/轮次 ID、递增序号，以及接收、解释增量、工具元数据、最终结果、错误和完成事件。客户端仅在同时收到**有效 final、成功 done 和正常 EOF** 后确认方案；缺帧、迟到错误、断连或取消都不能把临时内容当作成功结果。

最终结果先经过候选事实核验与原子保存，再进入成功终态。公开解释流仅基于预算、数量、金额等四个数字生成，不暴露 ReAct 内部推理、原始提示或工具正文。临时解释和工具状态不作为下单依据。

RPC 流有不超过 30 秒的传输 deadline、分段执行预算和有界背压；取消会向下游传播，但不保证不响应取消的依赖立刻停止，更不保证供应商不再计费。用量缺失或未完成时记 `unknown`，估算不能当作真实账单。协议细节见[流式设计归档](archive/agent-development.md#106-m53a-网关与网页版本化接入)。

## 检索与实时核验

- 默认策略 `RAG.Retrieval.Strategy: vector_first`。`hybrid_rrf` 为显式可选实验，双路有界并发，候选不足最多扩召回一次；不是默认自动切换策略。
- 默认 `TopK=10`、`InitialK=16`、`MaxK=64`、检索时限 3000 ms。混合策略及需求 RAG 执行要求最终 `TopK <= 32`，具体配置检查以实现为准。
- 后台同步只读 Mall `ScanProductIndex` 的完整一致快照。源端上限为 30 秒 / 5 万 SKU / 32 MiB；完成帧和正常 EOF 才能授权后续发布或清理，超限不能截断后冒充完整。
- 目标索引同步持有专用 PostgreSQL 会话锁；需直连或保持会话语义的连接池，不支持 transaction pooling。升级前先停旧同步器，不能假定旧版本遵守新锁协议。
- 配置 Mall 后，返回与保存前始终校验 SKU 的当前价格、库存等事实；价格变动需重新执行预算选择。索引中的事实不是最终交易依据。
- 源快照一致性、目标同步互斥、最终候选核验分别解决不同问题；都不等同于库存预占。

完整协议与故障范围见[索引设计归档](archive/agent-development.md#210-后台索引配置m31--m32)。

## 结构化需求执行

`DemandExecution.Mode` 默认为 `disabled`；`demo` 仅允许无 Mall 的显式演示环境，`mall` 要求新版 Mall 分类契约及审核过的分类数据。`DemandExecution.Retrieval` 默认 `keyword`，只有显式设为 `rag` 才复用上述 RAG 策略。

先调用 `intent/plan`，再用同一会话最新且可执行的 `plan_turn_id` 调用 `intent/execute`。执行请求不接收新的查询或预算覆盖，不能绕过计划确认；真实 Mall 路径保留双次用户鉴权与事实核验。

分类表由[独立迁移脚本](../scripts/migrations/20260918_product_demand_categories.sql)管理，不自动迁移或回填。输出会标记候选窗口、搜索受限、缺失类别等证据；不能把有界搜索结果写成全库最优解。

## 文件与 MCP 工具

文件工具和 MCP 默认均关闭。开启它们不应成为普通推荐的启动前提。

| 工具 | 已有控制 | 尚未覆盖 |
| --- | --- | --- |
| 文件 | Linux 后端按认证用户隔离；默认只读；写入另需 `AllowWrite` 与本轮首行 `/save <相对路径>`，仅创建不覆盖；单次读写默认 64 KiB | 用户累计空间 / 文件数配额等治理 |
| MCP | 已安装可信可执行文件的绝对路径；精确工具白名单且声明 `readOnlyHint=true`；最小子进程环境、超时和进程组回收 | OS / 网络沙箱、进程并发配额；只读声明不是安全证明 |

历史对话或检索文本中的指令不能充当本轮写入授权。工具记录只展示受控元数据；项目其他服务仍有敏感日志风险，见[权限与安全](access-control.md)。

## 开发与验证

从仓库根目录运行以下离线评测；它们不加载 `.env`，不调用真实模型或数据库：

```bash
go run ./services/rpc/agent/cmd/eval -format markdown
go run ./services/rpc/agent/cmd/eval -suite scripted -format markdown
go run ./services/rpc/agent/cmd/eval -suite retrieval -format markdown
go run ./services/rpc/agent/cmd/eval -suite demand -format markdown
```

四套评测分别覆盖规则、Fake Model + 真实 ReAct 容错、固定排序回放、合成需求组合；不能合并为真实语义质量分数。数据和原始报告见[评测目录](../services/rpc/agent/testdata/eval/)，质量与人工复核状态统一见[项目状态](status.md)。

人工复核准备工具可生成新目录：

```bash
go run ./services/rpc/agent/cmd/eval-review -out /tmp/agent-review-001
```

目标目录必须不存在；`-check <review.json>` 仅校验记录，不认证复核人身份或自动通过人工验收。不得改写旧报告、代填人工记录，或移动已知保留集来制造盲测结果。

Go 集成环境及跳过规则见 [CI 指南](CONTRIBUTION.md#go-检查与测试环境)。日常开发不需要常驻测试库；清理性测试不能指向业务库或保留验收库。

保留真实记录应使用 [dev-records](../services/rpc/agent/cmd/dev-records/) 的显式入口：默认只读，使用 `-env` 时必须明确 `-dsn-key`；写入另需目标库校验、已有用户和写入参数。启动服务、同步索引、真实模型调用、数据库迁移和部署都需按各自范围核对，不能把离线评测授权扩大为这些操作。
