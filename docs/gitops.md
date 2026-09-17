# 服务器 Argo CD 自动部署

目标服务器 `ubuntu@51.79.164.39`，现有 K3s 命名空间 `budgetmatch-sim`，业务入口仍为 `http://51.79.164.39:8080/`。80/443 保留给现有 Headlamp，不公开 Argo CD 管理端口。

发布链路：

```text
refactor/agent 的代码或部署配置 push
  → GitHub Actions「Deploy VPS」验证部署工具并构建两份镜像
  → 推送 GHCR，以镜像 digest 生成清单并更新 gitops 分支
  → 服务器 Argo CD 自动同步应用
```

当前发布源是 `refactor/agent`，不是较旧的 `main`。切换发布分支时须同时修改 `.github/workflows/deploy.yml` 的 `push.branches` 和 job `if`。仅改文档不会触发发布。`gitops` 是流水线维护的产物分支，不能手改；只有两份镜像都成功后才发布新清单，旧源码构建若已过期会跳过发布。

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
ssh ubuntu@51.79.164.39 'sudo -n bash -s' < deploy/bootstrap-server.sh
```

要求服务器已有本项目的 K3s 部署、`curl` 和免密 sudo。脚本先备份数据库、现有资源和 runtime Secret，再安装固定版本 Argo CD `v3.5.3`、Sealed Secrets `v0.40.0`，下载文件校验 SHA-256。关闭暂不使用的 Dex、ApplicationSet、Notifications，限制单应用场景的资源占用。

备份在服务器 `/var/backups/budgetmatch-gitops/<时间>/`，仅 root 可读；其中 `sealing-keys.yaml` 是解密能力所在，必须另做加密异机备份。Sealed Secrets 会续期密钥，后续新增密钥也需要备份。不要提交这些备份。脚本不会自动创建 Application，也不会更新业务服务；已有控制器不会被覆盖或降级。

### 3. 发布首个镜像并确认拉取权限

将部署文件提交、推送到 `refactor/agent` 后，查看 GitHub Actions 的 **Deploy VPS**。流水线使用自带 `GITHUB_TOKEN`，无需把服务器 SSH 密钥交给 GitHub；仓库须允许 `contents: write`、`packages: write`，`gitops` 分支规则须允许 Actions 写入。

构建生成：

- `ghcr.io/acmyuechen/budgetmatch-sim-backend`
- `ghcr.io/acmyuechen/budgetmatch-sim-web`

**GHCR 首次发布的包默认私有，即使源仓库公开。** 第一次接管前，选择下面一种方式，否则新 Pod 将出现 `ImagePullBackOff`：

- 允许公开镜像：在 GitHub 个人主页 **Packages → 对应包 → Package settings → Change visibility** 将两个包设为 Public。镜像构建不包含本机 `.env`。
- 保持私有：在服务器创建只读 GHCR 拉取凭据 Secret `ghcr`，命名空间为 `budgetmatch-sim`；使用有 `read:packages` 权限的凭据。在 `deploy/vps.yaml` 设置 `imagePullSecrets: [ghcr]` 并重新发布。不要提交明文拉取凭据。

从 `gitops` 分支的 `release.json` 取得两份完整 digest 地址，在服务器先确认两份镜像都能拉取。公开镜像可用 `sudo k3s ctr images pull <完整镜像地址>`；私有镜像需按 `ghcr` Secret 的认证方式验证。确认前不要创建 Application。

### 4. 接管现有应用

确认 `gitops` 分支存在、两份镜像可拉取，再执行：

```bash
ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl apply -f -' < deploy/argocd/project.yaml
ssh ubuntu@51.79.164.39 'sudo -n k3s kubectl apply -f -' < deploy/argocd/application.yaml
ssh ubuntu@51.79.164.39 'sudo k3s kubectl -n argocd get applications; sudo k3s kubectl -n budgetmatch-sim get pods'
```

Application 应最终显示 `Synced`、`Healthy`，业务入口可打开。第一次接管前的完整数据库备份由步骤 2 保存；若间隔较久，应先补一次新备份。

## 日常更新

**代码和普通配置：** 修改代码或 `deploy/vps.yaml`，提交并推送发布分支即可。配置优先使用 `deploy/vps.yaml` 的服务覆盖项，不要把密码填进去。渲染器拒绝常见凭据字段的明文值；配置摘要变化会自动重启对应 Pod。

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
python3 deploy/seal-alipay.py --cert deploy/.local/sealed-secrets.pem --kubeseal deploy/.local/kubeseal
```

只将生成的 **密文** `deploy/secrets/alipay.json` 提交并推送。Sealed Secrets 在服务器解密到独立的 `alipay` Secret，Argo CD 等待密钥就绪后自动更新 `payment-rpc`。本机 `.env` 和任何私钥都不提交。未添加此密文文件前，支付继续使用原有 `runtime` Secret 中的值；不意味着支付宝已可支付。加密脚本只做格式检查，AppID、密钥与商户是否配套仍须沙箱联调验证。

## 打开管理界面

```bash
bash deploy/open-ui.sh
```

打开 `https://localhost:8086`，接受本次 SSH 隧道连接的自签名证书提示；本机原 Argo CD 的 `8085` 不受影响。账号 `admin`，初始密码只在自己的终端查看：

```bash
ssh ubuntu@51.79.164.39 "sudo k3s kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}'" | base64 -d; echo
```

首次登录后修改密码，再删除 `argocd-initial-admin-secret`。管理界面可看资源、差异、同步状态及日志。不要为方便而把管理端口直接暴露到公网。

## 回退与排障

常规回退：在源码发布分支 revert 问题提交，再 push 生成新版本。由于自动同步开启，不要依赖 UI 的临时回滚或手工修改 Deployment，下一次同步会恢复 Git 声明的状态。

紧急暂停自动同步：

```bash
ssh ubuntu@51.79.164.39 'sudo k3s kubectl -n argocd patch application budgetmatch-sim --type merge -p '\''{"spec":{"syncPolicy":{"automated":{"enabled":false}}}}'\'''
```

暂停后再按服务器备份恢复需要的配置/版本；不要删除命名空间或 PVC，也不要重新运行旧部署生成器来“更新”数据库密码。排障完成后将 `enabled` 改回 `true`；先确认 `gitops` 已指向修复版本，避免再次同步故障配置。

本地验证：

```bash
python3 -m unittest discover -s deploy -p 'test_*.py' -v
bash -n deploy/bootstrap-server.sh deploy/open-ui.sh deploy/publish.sh
```

参考：[Argo CD 安装](https://argo-cd.readthedocs.io/en/stable/getting_started/)、[自动同步](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)、[Sealed Secrets](https://github.com/bitnami/sealed-secrets)、[GHCR 权限](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)。
