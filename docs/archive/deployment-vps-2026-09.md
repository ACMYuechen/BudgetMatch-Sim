# VPS 发布与外部数据源切换记录（2026-09）

> 历史快照：包含旧发布和 2026-09-21 新库切换的不同验收范围；命令不表示授权再次发布、迁移或重启。
> 当前入口：[VPS 运维](../deployment-vps.md)；[归档导航](README.md)。

服务器：`ubuntu@51.79.164.39:22`。

- 用户端：http://51.79.164.39:8080/
- 管理端：http://51.79.164.39:8080/admin
- 使用服务器现有 K3s，命名空间为 `budgetmatch-sim`。
- 发布目录为 `/opt/budgetmatch-sim/current`，完整运维说明为该目录内的 `DEPLOYMENT.md`。
- 原有 Headlamp 继续使用 80/443 端口，本应用通过 HTTP 8080 访问。

## 账户与数据

2026-09-21 按用户确认切换到全新外部数据库，不迁移旧业务数据。管理员用户名为 `admin`，新随机密码仅保存在服务器受保护的文件中：

```sh
cat /opt/budgetmatch-sim/current/credentials-external.json
```

当前 PostgreSQL 为 `39.108.76.53:5432`，账号 `nailong`，数据库 `postgres`；Redis 为 `39.108.76.53:6379`。初始化了 13 张业务表和 1 个新管理员，商品、订单、秒杀订单、Agent 会话均为空。旧发布的示例商品、订单、账号未导入；旧 `credentials.json` 只对应保留的旧库。

应用的数据库和 Redis 连接来自 Secret `external-data`，通过 `DATABASE_DSN`、`REDIS_ADDRESS`、`REDIS_PASSWORD` 注入。Secret `runtime` 继续保存 JWT、模型、邮箱等原配置及旧内部数据库凭据；不覆盖内部 Redis 的密码。`deploy/environments/vps.yaml` 的 `dataSecret` 和渲染器已同步此分离方式，后续发布必须包含这些改动，不能用旧清单切回内部数据源。

etcd、RocketMQ、Agent 工作目录和旧 PostgreSQL / Redis 的 PVC 保留，未删除旧库或旧卷。K3s 已启用开机启动，服务由 Deployment 管理。发布目录的 `apps.yaml`、`secrets.yaml`、`DEPLOYMENT.md` 已同步实际生效配置；`secrets.yaml` 和新管理员凭据均为 600，不应提交到仓库。
LLM 与邮箱配置沿用本地环境；支付宝沙箱与 Embedding 未配置，支付暂不可用，商品检索使用关键词模式。

切换沿用原镜像，没有部署本地最新 Agent 代码或调用外部模型。新 PostgreSQL 未启用 TLS，且没有 pgvector 扩展；向量检索保持关闭。跨公网数据连接仍需后续落实 TLS / 私网通道和来源 IP 限制，本次没有修改数据库服务器的防火墙或系统。

### 新库初始化与超时

首次自动建表在跨服务器连接上较慢，商城最后一张缺失表使用当前运行版本的**纯表结构**补齐，没有复制任何旧业务行。商城 `Database.AutoMigrate=false`，避免每次启动重复执行远程 DDL；以后升级商城表结构时，必须先备份并显式迁移，不能只发布镜像。

验收曾出现默认 2 秒 RPC 超时导致的查询失败和 401。App / Admin 的 `AuthRpc`、`SeckillRpc` 超时现为 10 秒，`MallRpc` 为 15 秒；Agent 原 60 秒配置保留。这只缓解已经观察到的跨服务器延迟，不代表网络性能已压测通过。

切换前资源、原运行 Secret 与发布文件备份位于服务器 `/var/backups/budgetmatch-data-cutover.242e66`（root 私有目录）。旧数据库仍在原卷中，但不再承载应用读写。Argo CD 自动同步仍关闭；后续手动发布应先核对新的 Secret 引用。

## 常用操作

在服务器上执行：

```sh
sudo k3s kubectl -n budgetmatch-sim get pods,svc,pvc
sudo k3s kubectl -n budgetmatch-sim logs deployment/app --tail=100
sudo k3s kubectl -n budgetmatch-sim logs deployment/agent-rpc --tail=100
sudo k3s kubectl -n budgetmatch-sim rollout restart deployment/app
```

当前外部 PostgreSQL 备份（借用保留的 PostgreSQL Pod 客户端，密码只经标准输入传递）：

```sh
umask 077
sudo k3s kubectl -n budgetmatch-sim get secret external-data -o jsonpath='{.data.DATABASE_PASSWORD}' |
  base64 -d |
  sudo k3s kubectl -n budgetmatch-sim exec -i deployment/postgres -- sh -c \
    'IFS= read -r PGPASSWORD; export PGPASSWORD PGCONNECT_TIMEOUT=8; exec pg_dump -h 39.108.76.53 -p 5432 -U nailong -d postgres -Fc' \
  > budgetmatch-external-postgres.backup
```

备份应另存到服务器之外。不要删除命名空间或 PVC，当前存储类会连同对应数据一起删除。

## 发布版本

业务基线为 `132ea3f3ca3b3c9afd17b75d04085fd923ad7003`，并包含本次联调发现的修复：

1. 商品列表的 `keyword` 参数允许省略或为空。
2. 认证中间件向推荐接口使用的请求上下文写入已验证的用户 ID。
3. App 到 Agent 的 RPC 客户端超时设为 60 秒。
4. 日志中间件保留底层响应的流式刷新能力，支持 SSE 推荐。

镜像与部署清单保存在服务器发布目录；具体镜像版本以 `apps.yaml` 为准。

### 后续 Agent 模型配置迁移（尚未在服务器执行）

`refactor/agent` 已增加 `LLM_THINKING` / `Model.Thinking`：使用 `deepseek-flash` 时必须显式为 `disabled`，其他兼容模型留空，关闭模型仍只需清空 `LLM_PROVIDER`。渲染器仅将这个新增环境变量的 Secret 引用设为可选；其它凭据引用仍必需。未使用 Flash 的旧 Secret 不必补键，Flash 缺失该值则由新应用在连接数据库前拒绝启动。

该模型迁移阶段只做了模板渲染和 SDK 本地检查；2026-09-21 的数据源切换没有发布新模型配置或新镜像。正式模型切换须在明确的发布窗口中，将支持该字段的新镜像、模型名与非思考配置配套核对，不单独把旧二进制切到 Flash。旧版本可能忽略新字段，不能认为保留 Flash 配置就能安全回滚；需回到保留安全修复的规则模式或已验收的兼容版本，且不删除会话/PVC。实际步骤与授权边界见 [Agent 最终收尾门槛](agent-development.md#131-最终收尾门槛)。历史发布验证不代表此模型迁移已上线。

## 验证结果

2026-09-21 新数据源验收：六个消费者 Pod 的实际数据库/Redis 环境变量与 `external-data` 一致，全部业务 Deployment 就绪；公网首页为 200。新管理员登录、用户信息、前后台空商品列表、秒杀活动列表、空会话列表均通过，匿名管理接口返回 401。新库确认只有 1 个新管理员，未创建订单或模型会话，未进行生产负载测试。

以下为旧库切换前的历史验收，不代表新库导入了相关数据：

已通过公网接口验证：管理员登录、用户信息、商品与后台目录、秒杀列表、下单与取消后的库存恢复、模型推荐、SSE 重放、会话持久化与删除、匿名访问拒绝。
真实 Chromium 浏览器已验证登录、用户商品页、后台商品页与流式推荐，未出现页面运行时错误。
对应商品参数解析、认证上下文、流式日志中间件及相关逻辑测试通过。
