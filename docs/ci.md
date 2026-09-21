# CI 运行说明

本文描述当前 [工作流](../.github/workflows/ci.yml) 和 [检查脚本](../scripts/ci/) 的实际行为。目录职责和发布入口见 [CI/CD 总览](cicd.md)。初版设计背景保留在 [CI 需求规格](requirements/ci.md)；其中尚未实现的目标不能视为已验收。

## 本地运行

在仓库根目录运行完整的 CI 检查：

```bash
scripts/ci/ci.sh
```

本机需要可用的 Docker daemon、Docker Compose、`go.mod` 声明版本的 Go（当前 1.26.8）、Node.js 20 和 npm。工具链与 Docker 基础镜像的同步方式见 [开发文档](../Contributors.md#工具链与依赖版本)。WSL 中只有 Docker 命令但未启用对应发行版的 Docker 集成，也不能运行完整容器检查。

也可以从仓库根目录单独执行：

```bash
scripts/ci/go-check.sh
scripts/ci/web-check.sh
scripts/ci/security-check.sh
scripts/ci/container-check.sh
```

当前 Makefile 没有 `make ci` / `make ci-go` 等目标；初版需求中的这些名称是建议入口，实际使用上面的脚本。

## 触发、检查范围与 CI Gate

- 工作流在 Pull Request 的 `opened`、`synchronize`、`reopened`、`ready_for_review`、`main` 分支 push 及手动 `workflow_dispatch` 时触发；没有定时触发器。向已有 PR 的来源分支推送新提交会触发 PR 的 `synchronize`。
- `Detect Changes` 根据 PR 目标分支到当前提交的完整差异，或 `main` push 的 `before..sha`，选择 Go、Web、安全检查及需要构建的镜像。不是仅看最后一次提交。
- 修改 `.github/`、`scripts/ci/`、`scripts/deploy/`、`deploy/`、`tests/cicd/` 或无法分类的路径，以及手动执行时，回退为全量检查；纯文档变更会跳过四类耗时检查，但仍执行变更检测和 `CI Gate`。
- `CI Gate` 要求所有被选中的检查成功，未被选中的检查必须为 `skipped`。例如 `Security Check` 失败会连带导致 `CI Gate` 失败，应先查看上游失败任务。
- 本仓库 `main` 的 push 或手动 CI 成功后，`Build VPS Images` 通过 `workflow_run` 构建该次 CI 的确切提交。PR、fork、失败或取消的 CI 不触发发布 job。镜像和候选清单自动生成，服务器部署仍需 Argo CD 手动 Sync。

## Go 检查与测试环境

Go 检查执行 `go mod download`、`go mod verify`、`go vet ./...`、竞态测试和 `go build ./...`。当前 `check_formatting` 调用被注释，**格式检查尚未作为门禁执行**；竞态测试使用 `go test -race`，未设置初版需求中的 `-count=1`。

脚本通过 `go list` 构建包依赖图。PR 和 `main` push 下只测试修改包及其反向依赖；手动执行、无事件上下文的本地执行、Go 依赖定义、CI 执行文件、Proto 或无法可靠映射的变更会回退到全量竞态测试。静态检查与构建始终覆盖整个仓库。

| 环境变量 | 用途及当前行为 |
| --- | --- |
| `ETCD_HOSTS` | 配置中心与分布式锁测试必需；未设置时 Go 检查脚本启动临时 etcd 容器 |
| `RAG_TEST_PG_DSN` | pgvector、商城订单事务、Outbox/Inbox、目录快照 MVCC、在线候选新鲜度集成测试共用连接，也是会话存储测试的备用 DSN；未设置时脚本启动临时 pgvector 容器。直接运行 `go test` 时，未配置会跳过相关数据库测试 |
| `AGENT_MEMORY_TEST_PG_DSN` | 会话存储测试优先使用的 DSN；未设置时使用 `RAG_TEST_PG_DSN` |

GitHub Actions 提供专用 etcd 和 pgvector PostgreSQL 16，并设置前两个变量；商城用例也使用这一现有连接，不再依赖另一套本地测试 DSN。商城订单/Outbox/Inbox 及目录测试分别创建并清理随机 schema，避免并发迁移互相影响。自行提供变量时只能连接可丢弃的测试环境：测试会改写键值或创建、删除表，不能使用业务库或生产服务。业务 `.env` 和 `.env.example` 不保存测试连接，测试不回退读取 `DATABASE_DSN`；`make test` 不负责启动这些依赖。

### 可选真实 Nginx 代理回归

[Agent Nginx 用例](../cmd/app/internal/logic/agent/agent_stream_nginx_test.go)默认跳过，仅在显式提供可信本机 Nginx 可执行文件绝对路径时运行：

```bash
BUDGETMATCH_TEST_NGINX_BIN=/usr/sbin/nginx \
  go test -race -p 1 -count=1 -run '^TestNginxRepositoryTemplateStream$' ./cmd/app/internal/logic/agent
```

自行替换为已安装/解包的路径；入口不下载软件，不需要 root，不加载系统 Nginx 配置或 `.env`。它在临时目录生成独立配置，仅将仓库 `web-ui/nginx/default.conf` 的监听、上游与静态目录替换为本次自有 loopback 目标，先运行 `nginx -t`，再启动并关闭自己的代理。模板结构变化会要求复核地址替换，错误的显式路径会失败而不是跳过。

真实 HTTP/TCP gRPC 搭配合成身份/生产者，覆盖增量及时转发、正常 EOF 终态屏障、迟到错误、缺失/非法终态和取消传播，断流不自动重跑 unary。该入口不连接数据库/模型，不等于 Auth、真实供应商、部署 Ingress 或饱和背压验收；本机真实服务与保留库的独立证据见 [Agent 第 11.16 节](agent.md#1116-m63b-本机真实-nginx-模板与保留库联调)。现有 CI 未安装/配置此入口，不能将默认 skip 算作 CI 已覆盖真实 Nginx。

## 前端检查

`Web Check` 目前只执行 `npm ci`、`npm run lint`、`npm run build`。仓库已有 Playwright 测试，但尚未纳入该 Job，不能把 Web Check 成功当作浏览器回归已运行。

本地在 `web-ui` 下另行执行：

```bash
npm ci
npx playwright install chromium
npm run test:e2e
```

浏览器系统库要求、模拟 API 范围与验收记录见 [前端路线图](frontend-roadmap.md#本地开发与验证)。这些测试不替代真实服务联调。

## 安全检查

- actionlint 检查工作流语法；Gitleaks 扫描当前目录的密钥泄漏，两者失败均阻断。
- govulncheck 检查当前 Go 工具链和模块依赖中的可达漏洞；可达漏洞阻断。只有未被当前代码调用的依赖漏洞提示时，不会因此阻断，但仍需持续关注。
- gosec 以高严重性、高置信度筛选，并按现有 `-no-fail` 策略报告发现；工具本身运行失败仍会阻断。
- Trivy 文件扫描报告 High/Medium/Low 问题，Critical 漏洞或配置问题阻断；构建后的镜像也由容器检查执行对应级别的扫描。

安全数据库会持续更新，同一份依赖可能在之后的运行中出现新告警。修复 Go 标准库漏洞需更新 `go.mod` 中的 Go 版本和 Docker 构建镜像；模块漏洞则升级对应依赖、整理锁文件并重跑验证。

本地 Gitleaks 使用目录扫描，`.gitignore` 不等于扫描排除规则，未提交的 `.env` 或日志也可能命中。对照远程结果时可使用不含本地密钥的干净检出；确认真实泄漏时应先撤销或轮换凭证并移除泄漏内容，不要上传本地密钥或全局禁用扫描。

## 容器检查

直接运行容器检查脚本时，默认构建全部 8 个应用镜像；PR 中只构建变更检测选中的镜像。在本地只检查指定镜像时，可以传入以空格分隔的目标列表：

```bash
CI_IMAGE_TARGETS="mall-rpc app" scripts/ci/container-check.sh
```

支持的目标包括 `auth-rpc`、`seckill-rpc`、`mall-rpc`、`agent-rpc`、
`payment-rpc`、`app`、`admin` 和 `web-ui`。

在 GitHub Actions 中，准备任务会验证 Compose 和 Dockerfile，并将公共 Go 依赖阶段导出到共享的 Buildx 缓存。
随后，选中的镜像会作为相互独立的 matrix 任务并行运行；每个任务都会读取后端公共缓存，并维护自己的目标构建缓存。

脚本不登录或推送镜像仓库。没有 Docker 时，原生 Go 构建或本地扫描器只能验证相应子项，不能据此宣称容器检查或整条远程 CI 已通过。

## CI/CD 工具回归

`tests/cicd/` 集中验证变更范围选择、CI Gate、就绪等待、工作流路径、成功 CI 与发布提交的对应关系、Argo CD 手动同步策略、清单渲染和支付宝加密输入。发布集成测试在临时目录中创建本地 Git 仓库，验证候选清单、版本历史和重复发布；不启动 Docker，也不连接 GitHub 或服务器：

```bash
python3 -m pip install -r deploy/requirements.txt
python3 -m unittest discover -s tests/cicd -p 'test_*.py' -v
for script in scripts/ci/*.sh scripts/deploy/*.sh; do
  bash -n "$script"
done
```

这些回归也由 **Build VPS Images** 在构建镜像前运行，不能替代业务测试或完整 CI。
