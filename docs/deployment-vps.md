# VPS 运维

[文档导航](README.md) · [GitOps 发布流程](gitops.md) · [历史发布与切库记录](archive/deployment-vps-2026-09.md)

以下为 2026-09-21 已记录的部署配置，不是服务器实时探测结果。操作前核对当前镜像、集群与 Secret 引用；本地提交或 CI 构建成功不代表生产已更新。

## 环境速查

| 项目 | 已记录配置 |
| --- | --- |
| 服务器 | `ubuntu@51.79.164.39:22`，现有 K3s |
| 命名空间 | `budgetmatch-sim` |
| 业务入口 | `http://51.79.164.39:8080/`，管理端 `/admin` |
| 集群管理 | Headlamp 使用原 80/443；不直接公开 Argo CD 管理端口 |
| 发布目录 | `/opt/budgetmatch-sim/current`，实际镜像以 `apps.yaml` 为准 |
| PostgreSQL | `39.108.76.53:5432`，数据库 `postgres`，用户 `nailong` |
| Redis | `39.108.76.53:6379` |

## Secret 与数据归属

- `external-data` 提供应用的 `DATABASE_DSN`、`REDIS_ADDRESS`、`REDIS_PASSWORD` 等外部数据连接；声明入口为 [vps.yaml](../deploy/environments/vps.yaml) 的 `dataSecret`。
- `runtime` 保留 JWT、模型、邮箱等运行配置及旧内部数据源凭据。不要把外部 Redis 密码覆盖到仍保留的内部 Redis。
- 新库管理员名为 `admin`，随机密码只保存在服务器的 `/opt/budgetmatch-sim/current/credentials-external.json`。旧 `credentials.json` 仅对应旧库；不要复制文件内容进文档或 Git。
- `secrets.yaml`、管理员凭据等私密文件应保持 `0600`。排查时检查引用和键是否存在，不输出 Secret / Pod 的完整环境变量。
- 最近一次按授权使用全新外部数据库，不迁移旧数据；旧 PostgreSQL / Redis PVC、etcd、RocketMQ 和 Agent 工作目录保留。**不要删除命名空间或 PVC**，对应存储可能随之删除。

切库时的备份位置、初始表/账号数量和只读验收范围见[切库归档](archive/deployment-vps-2026-09.md)，不能将当时的空表数量当作当前数据状态。

## 已部署版本的限制

上次数据源切换沿用原业务镜像，**没有部署本地最新 Agent 或切换 Flash / Embedding**。生产 PostgreSQL 当时没有 pgvector，向量检索保持关闭，使用关键词检索；支付宝沙箱配置也未完成，不能据此宣称支付可用。

商城 `Database.AutoMigrate=false`。后续表结构变更必须先备份、审核并显式迁移，不能仅更新镜像。App / Admin 的 AuthRpc、SeckillRpc 超时为 10 秒，MallRpc 为 15 秒；App → AgentRpc 保留 60 秒。这些设置只缓解已观察到的跨服务器延迟，不代表负载验收通过。

业务入口仍为 HTTP，外部 PostgreSQL 未启用 TLS。数据库来源 IP 限制、TLS / 私网通道、RPC 暴露收敛和业务权限修复仍需落实，见[权限与安全](access-control.md#待修复风险)。

## 只读排查

在目标服务器上执行：

```bash
sudo k3s kubectl -n budgetmatch-sim get deployments,pods,svc,pvc
sudo k3s kubectl -n budgetmatch-sim get deployments \
  -o custom-columns='NAME:.metadata.name,IMAGES:.spec.template.spec.containers[*].image'
sudo k3s kubectl -n budgetmatch-sim logs deployment/app --tail=100
sudo k3s kubectl -n budgetmatch-sim logs deployment/agent-rpc --tail=100
```

日志可能包含用户数据或尚未脱敏的认证信息，仅在受控终端检查，分享前脱敏。Pod Ready / TCP 探针正常不等于登录、支付、流式或数据库业务均通过。

## 发布前核对

1. 按 [GitOps 指南](gitops.md)核对成功 CI 的源码提交、两份镜像 digest 和待同步的 GitOps revision；Argo CD 保持人工确认后 Sync。
2. 确认新清单继续引用 `external-data`，不会把应用切回旧内部库；核对配置与当前镜像兼容性。
3. 对数据库迁移、模型切换和真实调用分别确认范围。Flash 需支持新字段的二进制与 `LLM_THINKING=disabled` 配套；启用 RAG 前补齐 pgvector、Embedding 和索引密钥。
4. 备份数据及当前配置，验证备份可恢复，并准备不删会话/PVC 的回退方案。旧二进制可能忽略新字段，不能假定保留 Flash 配置就可安全回滚。
5. 同步后验证实际 revision、服务就绪和真实业务路径，重点覆盖登录、用户隔离、流式终态、代理取消和新旧记录重放。

完整运维记录还位于服务器发布目录的 `DEPLOYMENT.md`。备份文件需私密保存并另存服务器之外；历史命令仅供审查，执行前重新核对实例、凭据来源和输出路径。剩余交付事项统一见[项目状态](status.md)。
