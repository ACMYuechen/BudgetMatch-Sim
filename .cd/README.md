# BudgetMatch-Sim CD 部署方案

> 方案状态：设计稿，尚未创建 Helm 清单、安装集群或完成上线验证。
>
> 适用分支：`chore/cd`。
>
> 目标路线：先以单节点 K3s + 非 HA Argo CD + GitOps 完成可重复部署，后续平滑扩展为三节点高可用集群。

## 1. 目标与边界

本方案负责定义 BudgetMatch-Sim 从镜像构建到 K3s 发布的完整 CD 链路，包括：

- 单节点 K3s 的安装、安全基线、入口和备份。
- Argo CD 的一次性 bootstrap 与后续声明式管理。
- BudgetMatch-Sim 的 Helm 资源模型。
- 应用源码仓库、镜像仓库和 GitOps 仓库之间的职责边界。
- Staging 验证、生产晋级、回滚和未来多节点迁移。

本方案不负责：

- 替代现有 CI 质量门禁。
- 把 PostgreSQL、Redis、RocketMQ 或业务 etcd 的数据写入 Git。
- 宣称单节点具备高可用能力。
- 在尚未确认域名、镜像仓库、外部状态服务和密钥方案前直接上线生产。

## 2. 已选架构

### 2.1 第一阶段

第一阶段采用以下组合：

| 层级 | 选择 | 说明 |
|---|---|---|
| Kubernetes | 单节点 K3s | 节省资源并验证 Kubernetes 部署模型，不提供节点级高可用 |
| 控制面数据 | 单成员 embedded etcd | 从第一天使用 `cluster-init`，减少未来从 SQLite 转换的步骤 |
| Ingress | K3s 内置 Traefik + ServiceLB | 单机直接使用 80/443，暂不引入 MetalLB 或额外网关 |
| CD | 非 HA Argo CD | 与业务运行在同一集群，初期通过端口转发访问 UI/API |
| 配置模板 | Helm | 一个 Chart 管理 8 个应用镜像和配套资源 |
| 期望状态 | GitOps 仓库 | 所有非敏感部署状态均从 Git 读取 |
| 镜像仓库 | GHCR 或 Harbor | 镜像使用 Git SHA 标签并最终锁定 digest |
| 状态服务 | 集群外部 | PostgreSQL/pgvector、Redis、RocketMQ、业务 etcd 不依赖 K3s 节点本地磁盘 |
| Secret | External Secrets 优先 | 暂无外部密钥管理器时，允许短期人工创建 Secret，但不得提交明文 |

### 2.2 目标拓扑

```text
Developer
    │
    ▼
BudgetMatch-Sim repository
    │ PR / main / tag
    ▼
GitHub Actions
    ├── test / build / scan
    ├── push 8 images ──────────────► GHCR / Harbor
    └── update image digests by PR
                                      │
                                      ▼
                            BudgetMatch-GitOps repository
                                      │
                                      ▼
                                   Argo CD
                                      │
                                      ▼
Internet ─► DNS/TLS ─► Traefik ─► single-node K3s
                                      ├── web-ui
                                      ├── app-api
                                      ├── admin-api (private only)
                                      ├── auth-rpc
                                      ├── seckill-rpc
                                      ├── mall-rpc
                                      ├── agent-rpc
                                      └── payment-rpc
                                               │
                                               ▼
                         external PostgreSQL / Redis / RocketMQ / business etcd
```

这里存在两套完全不同的 etcd：

- K3s embedded etcd：仅保存 Kubernetes 控制面对象。
- BudgetMatch 业务 etcd：用于当前 go-zero RPC 服务注册和业务配置。

两者必须使用独立端点、独立凭据和独立备份，业务程序不得连接 K3s 控制面的 etcd。

## 3. 落地前必须确认的输入

执行部署前必须填写下列信息。未确认项只能使用占位符生成模板，不能视为可上线配置。

| 项目 | 待确认值 | 建议默认值 |
|---|---|---|
| 服务器系统 | `<OS>` | 受支持的长期维护 Linux 发行版 |
| 服务器规格 | `<CPU/RAM/DISK>` | 外置状态服务时从 8C/16G/NVMe SSD 起步，最终以压测为准 |
| K3s 版本 | `<PINNED_K3S_VERSION>` | 选择受支持版本并固定，不跟随 latest |
| Argo CD 版本 | `<PINNED_ARGO_CD_VERSION>` | 固定发布版本，不直接长期跟随 stable 分支 |
| API 固定域名 | `<K3S_API_DNS>` | 从第一天写入 K3s `tls-san` |
| Web 域名 | `<WEB_DOMAIN>` | 例如 `www.example.com` |
| App API 域名 | `<API_DOMAIN>` | 例如 `api.example.com` |
| Admin 域名 | `<ADMIN_DOMAIN>` | 仅内网、VPN、零信任网关或 IP 白名单访问 |
| 镜像仓库 | `<REGISTRY>` | GHCR 或团队 Harbor |
| GitOps 仓库 | `<GITOPS_REPO_URL>` | 私有独立仓库，Argo CD 仅需只读权限 |
| Secret 管理 | `<SECRET_BACKEND>` | 云 Secret Manager、Vault 或 External Secrets 支持的后端 |
| 状态服务 | `<EXTERNAL_ENDPOINTS>` | 私网 DNS，不使用公网裸端口 |
| TLS 签发 | `<TLS_ISSUER>` | cert-manager + ACME 或企业内部 CA |
| RPO/RTO | `<RPO>/<RTO>` | 决定数据库、etcd 和对象存储备份频率 |

## 4. 当前仓库与部署准入门槛

### 4.1 当前状态

| 能力 | 当前状态 | CD 准入要求 |
|---|---|---|
| 代码质量检查 | 已有 | 保留 `.github/workflows/ci.yml` 作为合并门禁 |
| 8 个应用镜像构建 | 已有 | 复用当前 Dockerfile 和构建参数 |
| 镜像安全扫描 | 已有 | 发布流水线继续执行阻断级扫描 |
| 镜像推送 | 未实现 | 新建独立 `release.yml`，仅 main/tag 可推送 |
| Helm Chart | 未实现 | 必须覆盖 8 个服务、Ingress、配置、Secret 引用和迁移 Job |
| Kubernetes 清单验证 | 未实现 | 增加 `helm lint`、`helm template` 和 schema 校验 |
| 配置外部化 | 部分完成 | 清除生产配置中的本机地址和明文默认密码 |
| 健康检查 | 部分完成 | HTTP 区分 live/ready，RPC 增加标准 gRPC Health |
| 数据库迁移 | 服务启动时执行 | 改为单独 Job，应用副本关闭 AutoMigrate |
| RAG 同步 | agent-rpc 内启动 | 独立 Worker/CronJob 或 leader election |
| GitOps | 未实现 | 创建版本化部署仓库并由 Argo CD 拉取 |
| Secret 管理 | 未实现 | 明文不得进入 GitOps values 或 ConfigMap |

### 4.2 必须先完成的应用修改

在第一次 Argo CD Sync 前，至少完成：

1. 将所有生产数据库 DSN、Redis 地址和密码改成明确的环境变量占位符。
2. 清理 `127.0.0.1:<rpc-port>` 客户端地址，选择业务 etcd 或 Kubernetes Service DNS。
3. 增加 `/health/live` 与 `/health/ready`；RPC 服务注册标准 gRPC Health Service。
4. 增加独立 migration 命令入口，并在业务服务中设置 `AutoMigrate: false`。
5. 将 RAG 周期同步从 agent API 副本中分离，避免滚动更新时重复调用 Embedding。
6. 判断 `workspace/agent` 是否允许丢失：
   - 临时数据使用 `emptyDir`；
   - 业务数据改用 OSS/S3、数据库或可靠 CSI 存储。
7. 验证 SIGTERM、SSE 连接、gRPC 请求和消息消费者能够在 `terminationGracePeriodSeconds` 内退出。

即使 `replicas: 1`，`RollingUpdate` 的 `maxSurge: 1` 也可能在升级时短暂运行两个 Pod，因此 AutoMigrate 和 RAG 同步不能以“目前只有一个副本”为理由保留在 API 启动路径中。

## 5. Kubernetes 资源映射

### 5.1 应用工作负载

| 镜像目标 | 源入口 | 端口 | Kubernetes 资源 | 第一阶段副本 | 暴露范围 |
|---|---|---:|---|---:|---|
| `web-ui` | `web-ui` | 80 | Deployment + ClusterIP Service | 1 | 公网 HTTPS |
| `app` | `cmd/app` | 10002 | Deployment + ClusterIP Service | 1 | 公网 HTTPS，支持 SSE |
| `admin` | `cmd/admin` | 10001 | Deployment + ClusterIP Service | 1 | 私网/VPN/零信任入口 |
| `auth-rpc` | `services/rpc/auth` | 10003 | Deployment + ClusterIP Service | 1 | 集群内部 |
| `seckill-rpc` | `services/rpc/seckill` | 10004 | Deployment + ClusterIP Service | 1 | 集群内部 |
| `mall-rpc` | `services/rpc/mall` | 10005 | Deployment + ClusterIP Service | 1 | 集群内部 |
| `agent-rpc` | `services/rpc/agent` | 10006 | Deployment + ClusterIP Service | 1 | 集群内部 |
| `payment-rpc` | `services/rpc/payment` | 10007 | Deployment + ClusterIP Service | 1 | 集群内部 |

所有应用默认：

- 使用独立 ServiceAccount。
- 禁止 privileged、hostNetwork、hostPID 和 hostIPC。
- 使用非 root 用户；后端镜像已经以 UID 1000 运行。
- 声明 CPU/内存 requests 和 limits，初始值经过压测后再固化。
- 使用只读根文件系统时，为确需写入的 `/tmp` 或 Agent 临时工作区挂载 `emptyDir`。
- 不使用 `hostPath`、`nodeName` 或固定节点 IP。
- 镜像使用 `repository@sha256:digest`，禁止 `latest`。

### 5.2 状态服务

| 组件 | 第一阶段位置 | 连接方式 | 关键要求 |
|---|---|---|---|
| PostgreSQL/pgvector | 集群外 | 私网 DNS + TLS | 自动备份、PITR、连接池和 pgvector 扩展 |
| Redis | 集群外 | 私网 DNS +认证/TLS | 持久化策略、内存上限、驱逐策略和备份 |
| RocketMQ | 集群外 | 私网 NameServer/Broker | Broker 数据盘、消息保留和监控 |
| 业务 etcd | 独立实例/集群 | 私网 DNS + TLS | 不复用 K3s etcd，独立快照 |
| 对象存储 | OSS/S3 | HTTPS | 用户上传、Agent 持久文件和备份归档 |

如果因预算必须在同一台物理机运行状态服务，应明确标记为“单机演示环境”：它可以使用独立进程或 Compose，但不能被描述为高可用生产架构，也不能把本地卷直接当作未来多节点存储方案。

## 6. Namespace 与权限

建议使用：

```text
argocd                 Argo CD
budgetmatch-staging    首次自动化验证
budgetmatch-prod       生产命名空间
observability          监控、日志和追踪
external-secrets       Secret 同步控制器（如采用）
cert-manager           TLS 证书控制器（如采用）
```

单节点早期可只创建 `budgetmatch-staging`。正式生产前再创建 `budgetmatch-prod`；如果两者共享同一单节点，应通过 ResourceQuota、LimitRange 和独立 Secret 限制互相影响，但这仍不是故障域隔离。

Argo CD 使用专用 AppProject：

- 只允许指定 GitOps 仓库作为 source。
- 只允许部署到 `budgetmatch-*` 命名空间。
- 默认禁止应用创建 ClusterRole、ClusterRoleBinding、CRD 和 Namespace 等集群级资源。
- `platform` 控制器使用单独 AppProject 和更严格的审批。

## 7. Git 与目录设计

### 7.1 当前仓库中的 CD 目录

本分支先在 `CD/` 保存部署方案。后续实现建议形成：

```text
CD/
├── README.md
├── bootstrap/
│   ├── k3s/
│   │   ├── config.example.yaml
│   │   └── README.md
│   └── argocd/
│       ├── install.md
│       └── application-root.yaml
├── helm/
│   └── budgetmatch/
│       ├── Chart.yaml
│       ├── values.yaml
│       ├── values.schema.json
│       ├── values-staging.yaml
│       ├── values-single-node.yaml
│       └── templates/
├── argocd/
│   ├── projects/
│   └── applications/
└── runbooks/
    ├── deploy.md
    ├── rollback.md
    ├── backup-restore.md
    └── incident.md
```

### 7.2 GitOps 仓库

正式启用自动同步时，推荐把部署状态拆到独立私有仓库：

```text
BudgetMatch-GitOps/
├── bootstrap/
├── platform/
├── charts/
│   └── budgetmatch/
└── clusters/
    ├── staging/
    │   └── budgetmatch-application.yaml
    └── prod/
        └── budgetmatch-application.yaml
```

拆分原则：

- `BudgetMatch-Sim` 保存源码、Dockerfile、CI 和 Chart 的开发源。
- `BudgetMatch-GitOps` 保存经审核的环境值、镜像 digest 和 Argo CD Application。
- CI 不直接执行生产 `kubectl apply`，只创建 GitOps 变更 PR。
- Argo CD 对 GitOps 仓库使用只读 deploy key 或最小权限机器人凭据。
- GitOps 的 `main` 分支开启保护，生产值至少需要一名审核者批准。

第一阶段也可以让 Argo CD 直接读取本仓库 `CD/helm/budgetmatch`，但发布工作流必须配置 path filter，防止 CI 更新镜像 digest 后触发无限发布循环。部署稳定后应拆分独立仓库。

## 8. K3s 单节点 bootstrap

### 8.1 主机准备

在安装前确认：

- 主机名唯一且不会随重启变化。
- 时间同步正常。
- SSD/NVMe 有足够空间，容器镜像、日志和 embedded etcd 不共用即将写满的系统盘。
- 80/443 未被现有 Nginx、Apache、FRP 或其他进程占用。
- 22 仅允许管理网段；6443 仅允许管理端或内网访问。
- Pod/Service 网段不与宿主机、VPN 和外部状态服务网段冲突。
- DNS 已为 `<K3S_API_DNS>` 和业务域名预留记录。

### 8.2 K3s 配置

在安装前创建 `/etc/rancher/k3s/config.yaml`：

```yaml
write-kubeconfig-mode: "0600"

tls-san:
  - "<K3S_API_DNS>"

# 从第一天使用 embedded etcd，后续可直接加入第 2、3 个 server。
cluster-init: true

# Kubernetes Secret 在控制面存储中加密。
secrets-encryption: true
```

注意：单成员 embedded etcd 仍然是单点，只是减少未来控制面数据格式转换步骤。embedded etcd 依赖低延迟磁盘，不应部署在 SD 卡或不稳定网络盘。

安装时固定版本：

```bash
curl -sfL https://get.k3s.io \
  | INSTALL_K3S_VERSION="<PINNED_K3S_VERSION>" sh -
```

验证：

```bash
sudo k3s kubectl get nodes -o wide
sudo k3s kubectl get pods -A
sudo k3s kubectl get storageclass
sudo k3s kubectl get ingressclass
```

第一阶段保留 K3s 默认 CoreDNS、Traefik、ServiceLB、local-path-provisioner 和 metrics-server。local-path 只能用于允许丢失或明确绑定节点的数据，不承载 PostgreSQL、Redis、RocketMQ 等生产状态。

### 8.3 立即建立备份

安装完成后立即完成：

1. 保存 `/var/lib/rancher/k3s/server/token` 到受控密钥库。
2. 配置 embedded etcd 定时快照并上传 S3 兼容对象存储。
3. 设置保留周期、对象存储服务端加密和不可变保留策略。
4. 在非生产环境实际执行一次恢复演练。

只生成快照但从未验证恢复，不能视为具备灾难恢复能力。

## 9. Argo CD bootstrap

Argo CD 本身需要一次人工 bootstrap，这是 GitOps 链路的最小例外。第一阶段使用非 HA 安装。

```bash
ARGO_CD_VERSION="<PINNED_ARGO_CD_VERSION>"

sudo k3s kubectl create namespace argocd
sudo k3s kubectl apply \
  -n argocd \
  --server-side \
  --force-conflicts \
  -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGO_CD_VERSION}/manifests/install.yaml"
```

验证：

```bash
sudo k3s kubectl get pods -n argocd
sudo k3s kubectl rollout status deployment/argocd-server -n argocd
sudo k3s kubectl rollout status deployment/argocd-repo-server -n argocd
```

第一阶段不把 Argo CD UI 暴露公网：

```bash
sudo k3s kubectl port-forward \
  -n argocd \
  service/argocd-server \
  8080:443
```

初始化完成后：

1. 修改初始 admin 密码。
2. 删除 `argocd-initial-admin-secret`。
3. 为 GitOps 仓库添加只读凭据。
4. 创建限制 source、destination 和资源种类的 AppProject。
5. 使用 `https://kubernetes.default.svc` 作为同集群目标，不执行额外 `argocd cluster add`。

后续可以让 Argo CD 管理自身配置，但应在业务应用稳定后单独实施，避免 bootstrap 与自管理问题混在第一版。

## 10. Helm Chart 设计

### 10.1 values 分层

```text
values.yaml              安全通用默认值
values-staging.yaml      Staging 非敏感差异
values-single-node.yaml  单节点副本与资源覆盖
values-prod.yaml         生产非敏感差异
```

优先级建议：

```text
values.yaml
  < environment values
  < cluster-specific values
  < image digest updated by release PR
```

`values.schema.json` 必须验证：

- 镜像仓库和 digest 非空。
- `latest` 被拒绝。
- 副本数和资源值合法。
- 公网入口只允许 web/app/支付回调。
- Secret 只能引用 Secret 名称，不能接收明文密码。

### 10.2 镜像值

建议每个组件使用：

```yaml
images:
  app:
    repository: ghcr.io/<org>/budgetmatch-app
    digest: sha256:<digest>
  authRpc:
    repository: ghcr.io/<org>/budgetmatch-auth-rpc
    digest: sha256:<digest>
```

模板最终渲染为：

```text
ghcr.io/<org>/budgetmatch-app@sha256:<digest>
```

Git SHA 标签便于人类定位版本，digest 负责保证实际拉取内容不可变。

### 10.3 更新策略

无后台副作用的 Deployment：

```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1
```

配合：

- `readinessProbe` 成功后才接收流量。
- `preStop` 和足够的 `terminationGracePeriodSeconds`。
- SSE 网关停止接收新请求并等待已有连接结束。
- gRPC 服务在退出前停止注册、拒绝新请求并完成在途请求。

在 RAG 同步尚未拆分前，`agent-rpc` 不允许使用上述并行滚动策略；临时只能使用 Recreate 或人工停机升级。正式环境必须完成后台任务分离后再启用无损滚动。

### 10.4 探针

| 工作负载 | Liveness | Readiness | Startup |
|---|---|---|---|
| web-ui | HTTP 静态页或专用 health | HTTP 200 | 通常不需要 |
| app/admin | `/health/live` | `/health/ready` | 依启动耗时决定 |
| RPC 服务 | 标准 gRPC Health | 标准 gRPC Health readiness 状态 | agent 等慢启动服务建议启用 |

Liveness 不应依赖 PostgreSQL、Redis 或其他下游，否则依赖故障会造成所有 Pod 被反复重启。Readiness 可以反映服务尚未完成初始化，但必须避免高频、级联式探测全部下游。

### 10.5 数据库迁移

迁移资源使用独立 Job，并由 Argo CD PreSync hook 触发：

```yaml
metadata:
  annotations:
    argocd.argoproj.io/hook: PreSync
    argocd.argoproj.io/hook-delete-policy: BeforeHookCreation,HookSucceeded
```

要求：

- 迁移命令幂等。
- 迁移只运行一次，不随每个业务副本启动。
- 使用 expand/contract 兼容迁移，先兼容新旧应用，再删除旧字段。
- 迁移失败时停止本次 Sync。
- Git 回滚镜像不会自动回滚数据库，破坏性迁移必须提供单独恢复方案。

### 10.6 RAG 与消息任务

建议目标形态：

- `agent-rpc`：仅处理查询和向量检索。
- `rag-sync`：独立 CronJob 或单副本 Worker，承担商品全量/增量索引。
- `mall` RocketMQ 消费和 `seckill` Redis Stream 消费：第一阶段保持单副本；扩容前验证消费者组、幂等和积压恢复。

Embedding 调用有外部成本，RAG 同步必须提供执行次数、商品数、失败数、耗时和 token/调用量指标。

## 11. 配置和 Secret

### 11.1 ConfigMap 中允许保存

- `ETCD_HOSTS`
- `REDIS_ADDRESS`
- `ROCKETMQ_NAMESERVERS`
- `LLM_PROVIDER`、`LLM_MODEL`、`LLM_BASE_URL`
- `EMBEDDING_PROVIDER`、`EMBEDDING_MODEL`、`EMBEDDING_BASE_URL`
- `ALIPAY_NOTIFY_URL`、`ALIPAY_RETURN_URL`
- 日志级别、超时、连接池和 RAG 行为参数

### 11.2 Secret 中保存

- `DATABASE_DSN`
- `REDIS_PASSWORD`
- `JWT_SECRET`
- `EMAIL_PASSWORD`
- `LLM_API_KEY`
- `EMBEDDING_API_KEY`
- `PAYMENT_MALL_SERVICE_SECRET`
- `ALIPAY_PRIVATE_KEY`、`ALIPAY_PUBLIC_KEY`
- OSS AccessKey
- 私有镜像仓库拉取凭据

### 11.3 过渡方案

如果第一阶段尚无 External Secrets 后端，可以从受控主机上的专用 env 文件创建 Secret：

```bash
sudo k3s kubectl create namespace budgetmatch-staging
sudo k3s kubectl create secret generic budgetmatch-runtime \
  -n budgetmatch-staging \
  --from-env-file=/secure/path/budgetmatch-staging.env
```

该文件不得进入仓库，权限必须限制为所有者可读。人工 Secret 必须纳入备份和重建 runbook；完成 External Secrets 后移除此例外。

K3s `secrets-encryption` 只保护控制面存储中的 Secret，不代表可以把 Secret 明文提交到 Git。

## 12. 服务发现

第一阶段有两条可选路径，必须选择其一，不能继续依赖本地 `127.0.0.1` fallback。

### 12.1 保留业务 etcd

- RPC Server 注册到独立业务 etcd。
- API/RPC Client 通过同一业务 etcd 发现服务。
- `ETCD_HOSTS` 指向私网域名。
- 删除或禁用所有 `127.0.0.1:<rpc-port>` Endpoints。

优点是应用改动小；缺点是多维护一套业务 etcd。

### 12.2 使用 Kubernetes Service DNS

- 每个 RPC 服务创建 ClusterIP Service。
- Client 直接使用 `auth-rpc:10003`、`mall-rpc:10005` 等集群 DNS。
- RPC Server 不再依赖业务 etcd 注册。

优点是减少基础设施；缺点是需要验证 go-zero 当前配置和负载均衡行为。建议第一版保留业务 etcd，后续单独完成去 etcd 迁移。

## 13. Ingress、DNS 与 TLS

建议域名路由：

| 域名 | 后端 | 可见范围 |
|---|---|---|
| `<WEB_DOMAIN>` | `web-ui:80` | 公网 |
| `<API_DOMAIN>` | `app:10002` | 公网 |
| `<ADMIN_DOMAIN>` | `admin:10001` | 私网/VPN/零信任 |
| 支付回调 | `<API_DOMAIN>` 下的支付通知路径 | 公网，仅开放必要路由 |
| Argo CD | 第一阶段不配置公网域名 | 仅端口转发/管理网络 |

要求：

- HTTP 自动跳转 HTTPS。
- cert-manager 或受控证书负责 TLS。
- SSE 路由关闭不合适的代理缓冲并设置足够长的读取超时。
- 支付回调保留原始请求体和必要头部，防止代理改写影响验签。
- Admin 除网络限制外仍需应用身份认证，不能把 IP 白名单当作唯一鉴权。
- Traefik/ServiceLB 使用宿主机 80/443，安装前确认端口没有冲突。

## 14. NetworkPolicy

每个业务 Namespace 先应用默认拒绝，再按调用关系放行：

```text
Traefik -> web-ui / app / admin(受限入口)
app -> auth-rpc / seckill-rpc / mall-rpc / agent-rpc / payment-rpc / Redis / business etcd
admin -> auth-rpc / seckill-rpc / mall-rpc / business etcd
RPC -> required PostgreSQL / Redis / RocketMQ / business etcd / external APIs
agent-rpc -> LLM / Embedding / MCP allowlist / OSS
payment-rpc -> Alipay endpoints / mall-rpc
```

DNS、时间同步、镜像拉取和必要外部 API 也需要显式考虑。NetworkPolicy 上线应先在 Staging 观察拒绝日志，避免一次性切断未知依赖。

## 15. Argo CD Application 与同步策略

### 15.1 首次 Application

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: budgetmatch-staging
  namespace: argocd
spec:
  project: budgetmatch
  source:
    repoURL: <GITOPS_REPO_URL>
    targetRevision: main
    path: charts/budgetmatch
    helm:
      valueFiles:
        - values-staging.yaml
        - values-single-node.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: budgetmatch-staging
  syncPolicy:
    automated:
      enabled: false
    syncOptions:
      - CreateNamespace=true
```

首次部署必须人工 Sync。完成健康检查、接口冒烟、支付回调和回滚演练后，再通过 Git 改为：

```yaml
syncPolicy:
  automated:
    enabled: true
    prune: true
    selfHeal: true
  syncOptions:
    - CreateNamespace=true
    - ApplyOutOfSyncOnly=true
  retry:
    limit: 3
    backoff:
      duration: 10s
      factor: 2
      maxDuration: 3m
```

自动 prune 开启前必须确认：

- PVC、Secret、证书和外部资源删除策略明确。
- Helm 模板不存在随机值导致的持续 OutOfSync。
- 不通过 UI 或 `argocd app set` 保存长期生产参数，所有持久参数回写 Git。

### 15.2 同步顺序

建议使用 Argo CD sync wave：

| Wave | 资源 |
|---:|---|
| -3 | Namespace、ServiceAccount、RBAC、NetworkPolicy 基础 |
| -2 | ConfigMap、Secret 引用、ExternalSecret |
| -1 | 数据库 migration Job |
| 0 | RPC Deployment/Service、Worker |
| 1 | App/Admin/Web Deployment/Service |
| 2 | Ingress、证书和外部入口 |
| 3 | PostSync smoke test Job |

## 16. 发布流水线

保留现有 `ci.yml` 作为 PR 门禁，新增 `.github/workflows/release.yml`。

### 16.1 触发策略

- 合并到 `main`：构建候选镜像，可自动晋级 Staging。
- 版本 Tag：创建生产候选版本。
- `workflow_dispatch`：只允许受控重建，不允许任意覆盖生产 digest。

### 16.2 最小权限

发布工作流只申请：

- `contents: read`
- `packages: write`
- `id-token: write`（使用 keyless 签名时）

更新 GitOps 仓库使用独立 GitHub App 或最小权限 token，只允许创建 PR，不直接绕过分支保护。CI 不持有 K3s kubeconfig、Argo CD admin token或生产节点 SSH root 凭据。

### 16.3 Job 流程

```text
CI gate success
  -> build matrix (8 images)
  -> vulnerability scan
  -> generate SBOM
  -> sign images
  -> push Git SHA tags
  -> resolve registry digests
  -> update GitOps values
  -> open promotion PR
  -> reviewer approves and merges
  -> Argo CD detects commit
  -> sync Staging
  -> PostSync smoke test
  -> production promotion PR
```

镜像构建继续复用当前目标名：

```text
auth-rpc
seckill-rpc
mall-rpc
agent-rpc
payment-rpc
app
admin
web-ui
```

GitOps 更新必须只修改 digest 和发布元数据，不在 CI 中动态生成密码或其他不可重现值。

## 17. 可观测性

### 17.1 第一阶段最低要求

- 所有应用日志写 stdout/stderr，不依赖节点本地日志文件作为唯一证据。
- 采集 K3s 节点、Pod、Deployment、Ingress 和 embedded etcd 基础指标。
- 记录 HTTP/gRPC 请求量、错误率、延迟和在途请求。
- 记录 PostgreSQL连接池、Redis错误、RocketMQ积压和业务 etcd延迟。
- 记录 LLM/Embedding 调用量、耗时、错误和成本相关指标。
- Argo CD SyncFailed、Health Degraded 和证书到期进入告警。

单节点资源有限时，Prometheus/Grafana/Loki/Tempo 全套可能与业务争抢资源。第一阶段可以使用轻量采集器把指标、日志和 Trace 发送到外部后端；是否集群内自建需根据资源预算决定。

### 17.2 发布观测窗口

每次发布至少观察：

- Deployment 是否在 deadline 内 Ready。
- 5xx/gRPC error 是否上升。
- SSE 断连率是否异常。
- 数据库连接数、慢查询和锁等待。
- Redis超时和缓存命中率。
- RocketMQ/Redis Stream积压。
- RAG同步和Embedding费用。
- 支付回调验签、幂等和补偿任务。

## 18. 备份与恢复

| 对象 | 备份方式 | 恢复验证 |
|---|---|---|
| K3s embedded etcd | 定时快照上传 S3 | 在隔离环境恢复控制面 |
| K3s server token | 密钥库离线备份 | 与快照一起验证 |
| PostgreSQL | 全量备份 + WAL/PITR | 恢复到新实例并执行数据校验 |
| Redis | 按业务持久化要求配置 | 验证会话、缓存和限流降级行为 |
| RocketMQ | Broker数据与配置备份 | 验证消息重放和消费者幂等 |
| 业务 etcd | 独立快照 | 恢复服务注册/配置到隔离集群 |
| GitOps | Git托管 + 镜像/仓库备份 | 从空集群重建非敏感资源 |
| Secret 后端 | 后端原生备份 | 验证 ExternalSecret重新生成 |

恢复优先级：

```text
网络/DNS
  -> K3s 控制面
  -> Argo CD
  -> Secret 控制器
  -> 外部状态服务
  -> migration 状态确认
  -> RPC 服务
  -> App/Web/Admin
  -> Ingress/TLS
  -> smoke test
```

## 19. 回滚策略

### 19.1 应用回滚

标准回滚通过 Git：

1. 对 GitOps 中的镜像 digest 变更执行 `git revert`。
2. 审核并合并回滚 PR。
3. Argo CD 同步旧 digest。
4. 观察健康状态和业务指标。

自动同步开启时，不把 Argo CD UI 中的临时 rollback 当作最终状态，否则下一次 reconcile 会重新应用 Git 中的新版本。

### 19.2 数据库回滚

- 仅通过镜像回滚不能撤销数据库结构变化。
- 默认采用向后兼容的 expand/contract 迁移。
- 删除字段、缩窄类型、批量数据重写等操作必须单独审批并有备份。
- 恢复数据库前先停止写入或进入维护模式，避免恢复点之后的新数据被覆盖。

### 19.3 集群回滚

- K3s 和 Argo CD升级前固定版本并创建快照。
- 控制面故障按经过演练的 etcd snapshot + server token流程恢复。
- GitOps负责重建声明式资源，但不负责恢复业务数据库内容。

## 20. 单节点到多节点迁移

第一阶段即使用 embedded etcd 和固定 API DNS，迁移时执行：

1. 对 K3s embedded etcd 和 server token做可恢复备份。
2. 在固定 API DNS 前加入 HAProxy、kube-vip 或受控负载均衡地址。
3. 准备两个额外 server，保证主机名、网络参数和 K3s关键 flags一致。
4. 使用相同 token 将第 2、3 个 server加入 embedded etcd集群。
5. 验证三成员 quorum、快照和故障切换。
6. 增加 worker或允许 server承载业务工作负载。
7. 将 App/Web/RPC副本扩为至少2，并加入 topology spread和反亲和。
8. 切换 Argo CD HA安装，使 repo-server、server、controller和Redis缓存具备冗余。
9. 为关键应用增加 PDB；单副本工作负载不设置无法满足的 `minAvailable: 1`。
10. 执行节点逐个下线、入口切换和恢复演练。

外部 PostgreSQL、Redis、RocketMQ和业务 etcd地址保持不变，因此应用数据不参与 K3s节点搬迁，这是降低迁移难度的核心。

## 21. 分阶段实施计划

### Phase 0：部署准入整改

- [ ] 配置环境变量化。
- [ ] 清理本地 RPC Endpoints。
- [ ] 完成 live/ready和gRPC health。
- [ ] 增加 migration命令并关闭服务AutoMigrate。
- [ ] 分离RAG同步。
- [ ] 明确Agent工作区存储语义。
- [ ] 验证优雅退出。

完成标准：应用可以在没有本机依赖和固定节点目录的容器中启动、探测和退出。

### Phase 1：Helm 与静态验证

- [ ] 创建Chart和values schema。
- [ ] 覆盖8个应用镜像。
- [ ] 增加Service、Ingress、Secret引用、NetworkPolicy和migration Job。
- [ ] CI执行`helm lint`、`helm template`和策略检查。
- [ ] 所有镜像使用不可变版本。

完成标准：无需真实集群即可生成确定性、无明文密钥的完整清单。

### Phase 2：单节点平台

- [ ] 确认域名、端口和外部状态服务。
- [ ] 安装固定版本K3s。
- [ ] 建立etcd快照和server token备份。
- [ ] 安装固定版本Argo CD。
- [ ] 配置AppProject和GitOps仓库只读凭据。

完成标准：Argo CD可以在不暴露公网管理面的情况下读取GitOps仓库。

### Phase 3：Staging GitOps

- [ ] 首次人工Sync。
- [ ] 执行migration和8个工作负载部署。
- [ ] 验证TLS、SSE、RPC、RAG、消息和支付回调。
- [ ] 完成Git revert回滚演练。
- [ ] 开启selfHeal，最后再开启prune。

完成标准：Git提交是Staging期望状态的唯一持久来源。

### Phase 4：生产发布

- [ ] 新增镜像推送与签名流水线。
- [ ] 使用GitOps PR完成Staging到Prod晋级。
- [ ] 设置生产审批、Sync Window和告警。
- [ ] 完成数据库及集群恢复演练。
- [ ] 记录RPO、RTO和责任人。

完成标准：CI不直接访问生产集群，发布、回滚和审计均能通过Git证明。

### Phase 5：多节点与高可用

- [ ] 扩展三server K3s。
- [ ] 建立固定控制面和入口负载均衡。
- [ ] Argo CD切换HA。
- [ ] 应用多副本、PDB和拓扑分散。
- [ ] 完成节点故障、网络分区和容量压测。

完成标准：任意单节点故障不导致控制面或关键无状态业务整体不可用。

## 22. 验收清单

### 22.1 集群

- [ ] `kubectl get nodes` 全部 Ready。
- [ ] K3s版本被固定并有升级记录。
- [ ] embedded etcd快照上传成功。
- [ ] server token已备份且访问受控。
- [ ] 80/443只由预期入口组件占用。
- [ ] 6443不暴露给非管理网络。

### 22.2 Argo CD

- [ ] Argo CD版本固定。
- [ ] UI未裸露公网。
- [ ] GitOps仓库凭据只读。
- [ ] AppProject限制source、namespace和资源种类。
- [ ] Application状态为Synced且Healthy。
- [ ] 手工漂移能被检测和恢复。

### 22.3 应用

- [ ] 8个应用Pod全部Ready。
- [ ] migration只执行一次并成功。
- [ ] 所有RPC通过Service或业务etcd解析，不使用127.0.0.1跨Pod调用。
- [ ] liveness不因下游故障触发重启风暴。
- [ ] readiness能阻止未初始化Pod接流量。
- [ ] SSE发布期间行为符合预期。
- [ ] RAG同步不会因滚动发布重复执行。
- [ ] 支付回调公网可达、验签成功且幂等。

### 22.4 发布与恢复

- [ ] 镜像可以追溯到Git commit。
- [ ] 部署锁定digest且无latest。
- [ ] CI无生产kubeconfig和节点root凭据。
- [ ] GitOps变更经过PR审批。
- [ ] Git revert能够恢复上一应用版本。
- [ ] PostgreSQL和K3s恢复演练成功。

## 23. 主要风险

| 风险 | 影响 | 缓解措施 |
|---|---|---|
| 单节点宕机 | 整个应用和Argo CD停止 | 外置数据、备份、固定DNS、后续三节点 |
| embedded etcd磁盘延迟 | 控制面卡顿或不可用 | SSD/NVMe、磁盘监控、避免资源打满 |
| AutoMigrate并发 | 锁表、迁移冲突 | 独立PreSync Job |
| RAG重复同步 | Embedding费用和数据库压力增加 | 独立Worker/CronJob或leader election |
| 明文Secret进入Git | 密钥泄露 | External Secrets/Sealed Secrets、扫描和分支保护 |
| 自动prune误删 | 业务资源丢失 | Staging验证、删除策略、人工审批后开启 |
| 本地workspace丢失 | Agent文件不可恢复 | 明确临时语义或迁移对象存储 |
| 全套观测占满单机 | 业务Pod被驱逐 | 资源限制、外部观测后端、容量监控 |
| 数据库迁移不可逆 | 镜像回滚失败 | expand/contract、备份、独立审批 |
| Traefik端口冲突 | Ingress无法启动 | 安装前审计80/443占用 |

## 24. 推荐提交顺序

建议在 `chore/cd` 后续拆成可审核的小提交或PR：

1. `docs(cd): add k3s argocd gitops deployment plan`
2. `chore(config): externalize runtime dependency settings`
3. `feat(health): add kubernetes readiness and grpc health checks`
4. `chore(migration): separate schema migration from service startup`
5. `refactor(agent): separate rag synchronization worker`
6. `chore(cd): add budgetmatch helm chart`
7. `chore(ci): validate helm manifests`
8. `chore(release): publish signed immutable images`
9. `chore(gitops): add staging application and promotion flow`

不要在同一个提交中同时修改应用运行逻辑、创建整套Helm、增加镜像推送和开启Argo自动prune；分阶段可以让每个风险点独立验证和回滚。

## 25. 官方参考

以下文档用于实施时复核具体版本参数；命令中的版本占位符必须在执行前替换为经过确认的固定版本。

- [K3s Quick-Start Guide](https://docs.k3s.io/quick-start)
- [K3s Configuration Options](https://docs.k3s.io/installation/configuration)
- [K3s High Availability Embedded etcd](https://docs.k3s.io/datastore/ha-embedded)
- [K3s Backup and Restore](https://docs.k3s.io/datastore/backup-restore)
- [K3s Networking Services](https://docs.k3s.io/networking/networking-services)
- [K3s Volumes and Storage](https://docs.k3s.io/add-ons/storage)
- [K3s Secrets Encryption](https://docs.k3s.io/security/secrets-encryption)
- [Argo CD Getting Started](https://argo-cd.readthedocs.io/en/stable/getting_started/)
- [Argo CD Installation](https://argo-cd.readthedocs.io/en/stable/operator-manual/installation/)
- [Argo CD Automated Sync](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)
- [Argo CD CI Automation](https://argo-cd.readthedocs.io/en/stable/user-guide/ci_automation/)
- [Argo CD Helm](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/)
- [Argo CD High Availability](https://argo-cd.readthedocs.io/en/stable/operator-manual/high_availability/)
