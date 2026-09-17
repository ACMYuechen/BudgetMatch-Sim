# BudgetMatch-Sim VPS 部署

服务器：`ubuntu@51.79.164.39:22`。

- 用户端：http://51.79.164.39:8080/
- 管理端：http://51.79.164.39:8080/admin
- 使用服务器现有 K3s，命名空间为 `budgetmatch-sim`。
- 发布目录为 `/opt/budgetmatch-sim/current`，完整运维说明为该目录内的 `DEPLOYMENT.md`。
- 原有 Headlamp 继续使用 80/443 端口，本应用通过 HTTP 8080 访问。

## 账户与数据

管理员用户名为 `admin`，随机密码仅保存在服务器受保护的文件中：

```sh
cat /opt/budgetmatch-sim/current/credentials.json
```

使用新数据库，已导入 6 个示例商品和 12 个 SKU。联调会留下备注为 `Deployment smoke test` 的已取消订单。

数据库、Redis、etcd、RocketMQ 与 Agent 工作目录均使用持久化卷。K3s 已启用开机启动，服务由 Deployment 管理。
运行密钥保存在 Secret `runtime` 中，发布目录的 `secrets.yaml` 权限为 600，不应提交到仓库。
LLM 与邮箱配置沿用本地环境；支付宝沙箱与 Embedding 未配置，支付暂不可用，商品检索使用关键词模式。

## 常用操作

在服务器上执行：

```sh
sudo k3s kubectl -n budgetmatch-sim get pods,svc,pvc
sudo k3s kubectl -n budgetmatch-sim logs deployment/app --tail=100
sudo k3s kubectl -n budgetmatch-sim logs deployment/agent-rpc --tail=100
sudo k3s kubectl -n budgetmatch-sim rollout restart deployment/app
```

PostgreSQL 备份：

```sh
umask 077
sudo k3s kubectl -n budgetmatch-sim exec deployment/postgres -- pg_dump -U budgetmatch -d budgetmatch-sim -Fc > budgetmatch-sim.backup
```

备份应另存到服务器之外。不要删除命名空间或 PVC，当前存储类会连同对应数据一起删除。

## 发布版本

业务基线为 `132ea3f3ca3b3c9afd17b75d04085fd923ad7003`，并包含本次联调发现的修复：

1. 商品列表的 `keyword` 参数允许省略或为空。
2. 认证中间件向推荐接口使用的请求上下文写入已验证的用户 ID。
3. App 到 Agent 的 RPC 客户端超时设为 60 秒。
4. 日志中间件保留底层响应的流式刷新能力，支持 SSE 推荐。

镜像与部署清单保存在服务器发布目录；具体镜像版本以 `apps.yaml` 为准。

## 验证结果

已通过公网接口验证：管理员登录、用户信息、商品与后台目录、秒杀列表、下单与取消后的库存恢复、模型推荐、SSE 重放、会话持久化与删除、匿名访问拒绝。
真实 Chromium 浏览器已验证登录、用户商品页、后台商品页与流式推荐，未出现页面运行时错误。
对应商品参数解析、认证上下文、流式日志中间件及相关逻辑测试通过。
