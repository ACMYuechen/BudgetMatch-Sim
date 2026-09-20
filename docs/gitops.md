# 自动构建与 Argo CD 手动发布

目标服务器 `ubuntu@51.79.164.39`，现有 K3s 命名空间 `budgetmatch-sim`，业务入口仍为 `http://51.79.164.39:8080/`。80/443 保留给现有 Headlamp，不公开 Argo CD 管理端口。

目录职责见 [CI/CD 总览](cicd.md)：操作脚本在 `scripts/deploy/`，环境配置在 `deploy/environments/`，Argo CD 声明在 `deploy/argocd/`。

发布链路：

```text
将代码或部署配置合入 main
  → GitHub Actions「CI」自动检查，CI Gate 通过
  → GitHub Actions「Build VPS Images」自动构建通过 CI 的同一提交
  → 推送 GHCR，以镜像 digest 生成清单并更新 gitops 分支
  → Argo CD 显示 OutOfSync，等待人工确认
  → 在 Argo CD 选择要发布的 GitOps 提交，点击 Sync
  → K3s 更新应用，检查资源健康和业务功能
```

**镜像构建自动，服务器部署手动。** `CI` 在 PR、`main` push 或手动运行时执行；`Build VPS Images` 只接受本仓库 `main` 的 push／手动 CI 成功后的 `workflow_run`。PR、fork、其他分支、失败或取消的 CI 均不能发布。检出、镜像标签和发布记录都使用成功 CI 的 `head_sha`；发布脚本再次校验事件、仓库、分支和检出提交。

`gitops` 是流水线维护的候选产物分支，不是源码分支，不能手改。只有两份镜像都成功后才发布新清单。构建成功不代表已经上线；人工 Sync 前，集群继续运行已部署版本。如果构建过程中 `main` 已有更新提交，旧版本会跳过发布，等待最新提交的 CI 和构建；需要重试时，在 **Actions → CI → Run workflow** 中选择 `main`，CI 成功后会自动构建镜像。

关闭自动同步也会关闭 Argo CD 的自动漂移修复。K3s 仍按已部署的 Deployment 重建故障 Pod，无需人工批准每次故障恢复。若服务器此前启用了自动同步，先完成下方迁移，再合入新的工作流。

Argo CD 只接管 8 个应用 Deployment 及其 Service、ConfigMap。现有 PostgreSQL、Redis、etcd、RocketMQ、PVC、`runtime` Secret 均不接管、不重新生成，自动删除关闭。沿用当前单副本 `Recreate` 策略，更新可能短暂中断服务；这不是零停机部署。

## 一次性接入

### 1. 本机能 SSH 登录服务器

在 WSL 终端执行，密码只在终端输入，不要放进仓库或聊天：

```bash
ssh-copy-id -i ~/.ssh/id_ed25519.pub ubuntu@51.79.164.39
ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl get nodes'
```

### 2. 安装服务器端控制器

在仓库根目录执行：

```bash
ssh ubuntu@51.79.164.39 'sudo -n bash -s' < scripts/deploy/bootstrap-server.sh
```

要求服务器已有本项目的 K3s 部署、`curl` 和免密 sudo。脚本先备份数据库、现有资源和 runtime Secret，再安装固定版本 Argo CD `v3.5.3`、Sealed Secrets `v0.40.0`，下载文件校验 SHA-256。关闭暂不使用的 Dex、ApplicationSet、Notifications，限制单应用场景的资源占用。

备份在服务器 `/var/backups/budgetmatch-gitops/<时间>/`，仅 root 可读；其中 `sealing-keys.yaml` 是解密能力所在，必须另做加密异机备份。Sealed Secrets 会续期密钥，后续新增密钥也需要备份。不要提交这些备份。脚本不会自动创建 Application，也不会更新业务服务；已有控制器不会被覆盖或降级。

### 3. 发布首个镜像并确认拉取权限

先将部署文件和两个工作流合入默认分支 `main`。GitHub 的 `workflow_run` 要求下游工作流文件存在于默认分支；只在开发分支修改不会建立自动构建入口。参见 [GitHub workflow_run](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run)。已有 Application 的服务器必须先按“从自动同步迁移”关闭自动同步。

等待 **CI → Build VPS Images** 成功，或手动运行一次 `main` 的 **CI**。镜像构建使用自带 `GITHUB_TOKEN`，无需把服务器 SSH 密钥交给 GitHub；仓库须允许 `contents: write`、`packages: write`，`gitops` 分支规则须允许 Actions 写入。流水线不会调用 Argo CD 或集群 API。

构建生成：

- `ghcr.io/acmyuechen/budgetmatch-sim-backend`
- `ghcr.io/acmyuechen/budgetmatch-sim-web`

**GHCR 首次发布的包默认私有，即使源仓库公开。** 第一次接管前，选择下面一种方式，否则新 Pod 将出现 `ImagePullBackOff`：

- 允许公开镜像：在 GitHub 个人主页 **Packages → 对应包 → Package settings → Change visibility** 将两个包设为 Public。镜像构建不包含本机 `.env`。
- 保持私有：在服务器创建只读 GHCR 拉取凭据 Secret `ghcr`，命名空间为 `budgetmatch-sim`；使用有 `read:packages` 权限的凭据。在 `deploy/environments/vps.yaml` 设置 `imagePullSecrets: [ghcr]` 并重新发布。不要提交明文拉取凭据。

从 `gitops` 分支的 `release.json` 取得两份完整 digest 地址，在服务器先确认两份镜像都能拉取。公开镜像可用 `sudo k3s ctr images pull <完整镜像地址>`；私有镜像需按 `ghcr` Secret 的认证方式验证。确认前不要创建 Application。

### 4. 接管现有应用

确认 `gitops` 分支存在、两份镜像可拉取，再执行：

```bash
ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl apply -f -' < deploy/argocd/project.yaml
ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl apply -f -' < deploy/argocd/application.yaml
ssh ubuntu@51.79.164.39 'sudo k3s kubectl -n argocd get applications; sudo k3s kubectl -n budgetmatch-sim get pods'
```

新建 Application 后仍需按下方“日常更新”手动 Sync。同步当前候选提交后，应最终显示 `Synced`、`Healthy`，并实际验证业务入口。第一次接管前的完整数据库备份由步骤 2 保存；若间隔较久，应先补一次新备份。

## 从自动同步迁移

**先改服务器策略，再合入自动构建工作流。** `application.yaml` 本身不在该 Application 监听的 `gitops/apps.yaml` 中，因此仅提交文件不会关闭线上自动同步。

1. 在 Argo CD 确认当前没有正在进行的同步操作；已有同步不会因关闭自动同步而被取消。
2. 从包含本次改动的检出中，将手动同步策略应用到服务器：

   ```bash
   ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl apply -f -' < deploy/argocd/application.yaml
   ssh ubuntu@51.79.164.39 "sudo k3s kubectl -n argocd get application budgetmatch-sim -o jsonpath='{.spec.syncPolicy.automated}'"
   ```

   确认 `enabled` 和 `selfHeal` 均为 `false`。保持当前业务版本，不执行 Sync。
3. 如果开发分支还保留旧版自动发布触发器，先移除旧触发器并检查正在运行的旧发布任务。只改 `main` 不会删除其他分支的工作流。
4. 将工作流改动合入 `main`，等待 CI 和镜像构建成功。确认 `release.json` 记录的源码提交正确后，再人工同步选定版本。

后续保持自动同步关闭，每次新版本都通过手动 Sync 发布。

## 日常更新

**代码和普通配置：** 修改代码或 `deploy/environments/vps.yaml`，合入 `main`，等待自动 CI 和镜像构建成功。配置优先使用 `deploy/environments/vps.yaml` 的服务覆盖项，不要把密码填进去。渲染器拒绝常见凭据字段的明文值；手动 Sync 应用配置后，配置摘要变化会重建对应 Pod。

发布操作：

1. 查看 **Build VPS Images** 的日志和摘要，记录 `Source commit`、`GitOps revision` 及两份镜像 digest。若日志提示旧提交被跳过，等待最新提交完成；绿色构建状态不能代替版本核对。
2. 通过下方脚本打开 Argo CD，进入 `budgetmatch-sim`，检查 Diff 和该 GitOps 提交的 `release.json`。`OutOfSync` 表示候选清单与当前部署不同，线上资源仍可能是 `Healthy`。
3. 点击 **Sync**，将 Revision 明确设为审核过的 **GitOps 提交 SHA**，不是源码 SHA；保持 Prune 未勾选，然后确认同步。Refresh 只刷新比较结果，不会部署。已登录 Argo CD CLI 时，也可执行 `argocd app sync budgetmatch-sim --revision YOUR_GITOPS_COMMIT_SHA`。
4. 检查同步操作实际使用的 revision、Deployment/Pod 就绪状态，并验证登录、商品查询等业务。当前 TCP 就绪探针不代表所有业务功能均正常。

同步最新候选提交后通常显示 `Synced`；若你有意发布历史候选，或随后又生成了新候选，应用可以保持 `Healthy` 同时显示 `OutOfSync`。流水线追加保存 GitOps 历史，便于选定版本；镜像构建和手动审批之间不重新构建镜像。

**支付宝沙箱：** Argo CD 无法读取你电脑的 `.env`。先核对沙箱 AppID（不能把 `2088…` 商家 PID 当 AppID），再用服务器公钥加密选定的六个 `ALIPAY_*` 字段；不复制本机数据库/JWT/Redis 密钥。

首次安装本机 `kubeseal` 并获取公钥：

```bash
mkdir -p deploy/.local
scp ubuntu@51.79.164.39:/usr/local/bin/kubeseal deploy/.local/kubeseal
scp ubuntu@51.79.164.39:/var/lib/budgetmatch-gitops/sealed-secrets.pem deploy/.local/sealed-secrets.pem
chmod 755 deploy/.local/kubeseal
python3 -m pip install -r deploy/requirements.txt
```

每次 `.env` 更新后，在 WSL 仓库根目录执行：

```bash
python3 scripts/deploy/seal-alipay.py --cert deploy/.local/sealed-secrets.pem --kubeseal deploy/.local/kubeseal
```

只将生成的 **密文** `deploy/secrets/alipay.json` 提交并合入 `main`，等待 CI 和镜像构建，再手动 Sync。同步后 Sealed Secrets 在服务器解密到独立的 `alipay` Secret，Argo CD 等待密钥就绪后更新 `payment-rpc`。本机 `.env` 和任何私钥都不提交。未添加此密文文件前，支付继续使用原有 `runtime` Secret 中的值；不意味着支付宝已可支付。加密脚本只做格式检查，AppID、密钥与商户是否配套仍须沙箱联调验证。

## 打开管理界面

```bash
bash scripts/deploy/open-ui.sh
```

打开 `https://localhost:8086`，接受本次 SSH 隧道连接的自签名证书提示；本机原 Argo CD 的 `8085` 不受影响。账号 `admin`，初始密码只在自己的终端查看：

```bash
ssh ubuntu@51.79.164.39 "sudo k3s kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}'" | base64 -d; echo
```

首次登录后修改密码，再删除 `argocd-initial-admin-secret`。管理界面可看资源、差异、同步状态及日志。不要为方便而把管理端口直接暴露到公网。

## 回退与排障

常规回退：在 `main` revert 问题提交，CI 和镜像构建成功后，人工 Sync 修复后的 GitOps 提交。也可以核对镜像仍可拉取、配置和数据兼容后，手动 Sync 历史 GitOps 提交。下一次手动同步仍需明确核对版本。

排障期间保持自动同步关闭，按需检查日志或依据服务器备份恢复配置。不要删除命名空间或 PVC，也不要重新运行旧部署生成器来“更新”数据库密码。构建失败不会产生新候选；同步失败先检查具体资源的事件和日志，再决定是否重试。

本地验证：

```bash
python3 -m unittest discover -s tests/cicd -p 'test_*.py' -v
for script in scripts/deploy/*.sh; do
  bash -n "$script"
done
```

参考：[Argo CD 安装](https://argo-cd.readthedocs.io/en/stable/getting_started/)、[自动同步](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)、[Sealed Secrets](https://github.com/bitnami/sealed-secrets)、[GHCR 权限](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)。
