# CI/CD 总览

CI 负责检查，CD 负责发布；工作流入口、执行脚本、部署配置和测试分开放置。

## 两个入口

| 入口 | 何时运行 | 做什么 | 是否更新服务器 |
| --- | --- | --- | --- |
| [CI](../.github/workflows/ci.yml) | PR 事件或手动运行 | 选择检查范围，测试、扫描并验证镜像构建 | 否，不推送镜像 |
| [Deploy VPS](../.github/workflows/deploy.yml) | **仅在 `main` 手动点击 Run workflow** | 验证发布工具，构建并推送镜像，生成 GitOps 清单 | 是，由 Argo CD 同步发布结果 |

推送或合并代码不会自动触发发布构建。发布流程仍是：

```text
代码合入 main
  → Actions / Deploy VPS / Run workflow（选择 main）
  → 构建后端和前端镜像，推送 GHCR
  → 更新 gitops 产物分支
  → Argo CD 同步服务器
```

`gitops` 只保存生成的清单，不是另一个源码发布分支。只有手动触发的 `main` 版本允许发布。

## 目录布局

```text
.github/workflows/
  ci.yml                       CI 调度入口
  deploy.yml                   main 手动发布入口
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
| 修改手动发布编排 | [deploy.yml](../.github/workflows/deploy.yml) |
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

完整检查范围和边界见 [CI 说明](ci.md)；真正发布时按 [GitOps 指南](gitops.md) 在 GitHub 手动触发。
