# CI/CD 总览

CI 负责检查，CD 负责发布；工作流入口、执行脚本、部署配置和测试分开放置。

## 三个入口

| 入口 | 何时运行 | 做什么 | 是否更新服务器 |
| --- | --- | --- | --- |
| [CI](../.github/workflows/ci.yml) | PR、`main` push 或手动运行 | 选择检查范围，测试、扫描并验证镜像构建 | 否，不推送镜像 |
| [Build VPS Images](../.github/workflows/deploy.yml) | 本仓库 `main` 的 push／手动 CI 成功后自动运行 | 构建并推送通过 CI 的同一提交，生成候选 GitOps 清单 | 否，等待手动 Sync |
| Argo CD 的 `budgetmatch-sim` 应用 | 人工检查版本和差异后点击 **Sync** | 将选定 GitOps 提交同步到 K3s | 是 |

发布流程：

```text
代码合入 main
  → 自动执行 CI，CI Gate 通过
  → Build VPS Images 构建后端和前端镜像，推送 GHCR
  → 更新 gitops 候选清单，Argo CD 显示 OutOfSync
  → 人工检查 GitOps 提交和差异，在 Argo CD 点击 Sync
  → K3s 更新应用
```

`gitops` 只保存生成的候选清单。构建固定使用触发它的成功 CI 的 `head_sha`，不使用 `workflow_run` 自身的默认分支 SHA。PR、外部 fork、失败或取消的 CI 均不能发布候选清单。需要重新构建时，在 Actions 的 **CI** 工作流选择 `main` 手动运行；通过后自动进入镜像构建。

现有服务器需先按 [迁移说明](gitops.md#从自动同步迁移) 关闭自动同步，再把这些工作流合入 `main`。仅修改仓库里的 Application 文件不会更新服务器上的同步策略。

## 目录布局

```text
.github/workflows/
  ci.yml                       CI 调度入口
  deploy.yml                   main CI 成功后的自动镜像构建入口
scripts/
  ci/                          CI 检查和结果汇总脚本
  deploy/                      初始化、渲染、发布、密钥加密、管理界面工具
  dev.sh、dev-stop.sh 等        原有本地开发脚本，保持原位
deploy/
  argocd/                      Application 和 AppProject 声明
  environments/vps.yaml        服务器非敏感配置及服务覆盖项
  images/backend.Dockerfile    服务器后端发布镜像，包含 7 个服务二进制
  requirements.txt             Python 工具依赖
  secrets/                     可选的 SealedSecret 密文，不放明文密钥
  .local/                      本机辅助工具和公钥，被 Git 忽略
tests/cicd/                    CI/CD 工具回归测试
docs/
  cicd.md                      本导航
  ci.md                        CI 检查内容、依赖和本地验证
  gitops.md                    服务器接入、手动发布、密钥和回退
```

`deploy/secrets/` 和 `deploy/.local/` 按需创建，不要求干净检出时存在。根目录 `Dockerfile` 继续用于单服务构建和 CI 容器检查；`web-ui/Dockerfile` 继续用于前端构建。根目录 `docker-compose.yml` 保持本地开发用途，不与服务器 Kubernetes 配置混放。

## 按任务找文件

| 要做的事 | 入口 |
| --- | --- |
| 修改 CI 触发器、依赖顺序和权限 | [ci.yml](../.github/workflows/ci.yml) |
| 修改检查范围选择 | [detect-changes.sh](../scripts/ci/detect-changes.sh) |
| 修改 Go、Web、安全或容器检查 | [scripts/ci/](../scripts/ci/) 中对应 `*-check.sh` |
| 修改门禁结果判定 | [gate.sh](../scripts/ci/gate.sh)，保留 `CI Gate` 检查名称 |
| 修改自动镜像构建编排 | [deploy.yml](../.github/workflows/deploy.yml) |
| 修改 Argo CD 手动同步策略 | [application.yaml](../deploy/argocd/application.yaml) |
| 修改服务器普通配置 | [vps.yaml](../deploy/environments/vps.yaml) |
| 修改 Kubernetes 清单生成 | [render.py](../scripts/deploy/render.py) |
| 修改产物分支发布逻辑 | [publish.sh](../scripts/deploy/publish.sh) |
| 加密支付宝配置 | [seal-alipay.py](../scripts/deploy/seal-alipay.py) |
| 初始化服务器或打开管理界面 | [scripts/deploy/](../scripts/deploy/)，操作前阅读 [GitOps 指南](gitops.md) |
| 添加 CI/CD 工具回归 | [tests/cicd/](../tests/cicd/) |

工作流保留环境准备和调度；较长的 Shell 逻辑放在脚本中。不要把服务器密码、`.env`、私钥或备份放进这些目录的受版本控制文件。

## 常用本地命令

以下命令从仓库根目录运行。

完整 CI 检查，需要 Docker、Go 和 Node 等依赖：

```bash
scripts/ci/ci.sh
```

轻量 CI/CD 工具回归，不构建业务镜像、不连接服务器：

```bash
python3 -m pip install -r deploy/requirements.txt
python3 -m unittest discover -s tests/cicd -p 'test_*.py' -v
for script in scripts/ci/*.sh scripts/deploy/*.sh; do
  bash -n "$script"
done
```

完整检查范围和边界见 [CI 说明](ci.md)；镜像准备好后，按 [GitOps 指南](gitops.md) 在 Argo CD 手动同步选定版本。
