# BudgetMatch-Sim CI 需求规格

> 状态：Draft
>
> 适用分支：`chore/ci`
>
> 最后更新：2026-08-08
>
> 范围：持续集成（CI），不包含部署（CD）

## 1. 文档目的

本文定义 BudgetMatch-Sim 当前阶段的 CI 最小可用方案，作为编写 GitHub Actions、仓库脚本和分支保护规则的实施依据。

当前阶段以“代码可检查、测试可执行、服务可构建、镜像可生成”为目标，不建设定时任务、复杂指标体系或高可用兜底机制。部署工具与部署流程另行调研和决策。

关键词含义：

- **MUST（必须）**：第一版 CI 的合并门禁要求。
- **SHOULD（应该）**：建议实现，但不阻塞第一版 CI 上线。
- **MAY（可以）**：出现明确需求后再实施。

## 2. 项目基线

仓库当前包含：

- 单一 Go Module，Go 版本以 `go.mod` 为准。
- 7 个 Go 可执行服务：
  - `cmd/admin`
  - `cmd/app`
  - `services/rpc/auth`
  - `services/rpc/seckill`
  - `services/rpc/mall`
  - `services/rpc/agent`
  - `services/rpc/payment`
- React/Vite 前端：`web-ui`。
- Go 测试依赖 etcd；pgvector 集成测试通过 `RAG_TEST_PG_DSN` 配置。
- 后端服务复用根目录 `Dockerfile`，前端使用 `web-ui/Dockerfile`。
- 根目录已有 `docker-compose.yml`。

## 3. CI 范围

CI **MUST** 验证：

1. Go 依赖、格式、静态检查和测试通过。
2. 7 个 Go 服务均可编译。
3. 前端依赖可安装，lint 和生产构建通过。
4. Compose 配置及两个 Dockerfile 可用于构建。
5. 8 个应用镜像均可成功构建。
6. 代码、依赖和镜像通过基础安全检查。
7. PR 不读取生产密钥，也不调用真实生产外部服务。
8. 开发者可以在本地运行与 CI 等价的核心命令。

## 4. 工作流设计

只建立一个核心工作流：

```text
.github/workflows/ci.yml
```

Job 结构：

```text
go-check ─────────┐
web-check ────────┤
security-check ───┼──> CI Gate
container-check ──┘
```

不同 Job 应并行执行。`CI Gate` 汇总所有必需检查，为分支保护提供唯一、稳定的 required check 名称。

## 5. 触发与权限

### 5.1 触发条件

核心 CI **MUST** 支持：

- Pull Request：`opened`、`synchronize`、`reopened`、`ready_for_review`。
- 推送到 `main`。
- 手动执行：`workflow_dispatch`。

同一 PR 出现新提交时，旧运行 **SHOULD** 通过 `concurrency` 取消，避免展示过期结果。

第一版 **MUST NOT** 配置 `schedule`。

### 5.2 最小权限

工作流默认权限 **MUST** 为：

```yaml
permissions:
  contents: read
```

第一版 CI 不推送镜像，因此不申请 `packages: write` 或 `contents: write`。

### 5.3 PR 安全边界

- PR Job **MUST NOT** 读取生产密钥。
- **MUST NOT** 使用 `pull_request_target` 执行 PR 中的代码。
- 分支名、PR 标题等不可信输入 **MUST NOT** 直接拼接到 shell 命令。
- CI **MUST NOT** 调用真实生产外部服务；相关测试应使用 Mock、Stub 或测试配置。

## 6. 必需检查

### 6.1 Go 检查：`go-check`

Go 版本 **MUST** 以 `go.mod` 为唯一来源。

至少执行：

```bash
go mod download
go mod verify
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 -race ./...
```

测试环境 **MUST** 提供：

- etcd，与仓库当前测试兼容。
- pgvector PostgreSQL 16。
- `ETCD_HOSTS` 和 CI 专用 `RAG_TEST_PG_DSN`。

pgvector 集成测试不得因 CI 缺少 DSN 而被静默跳过。

测试通过后，**MUST** 编译全部 7 个 Go 服务。构建输出仅用于验证，不作为长期 Artifact 保存。

`golangci-lint` 可在规则集确认后加入同一 Job；第一版不得为了通过检查而大范围排除业务目录。

### 6.2 前端检查：`web-check`

在 `web-ui` 中执行：

```bash
npm ci
npm run lint
npm run build
```

CI **MUST** 使用锁文件安装依赖，不得通过 `npm install` 修改锁文件。

项目当前没有前端自动化测试脚本，因此第一版不把 Vitest 作为验收条件；新增测试框架后再将 `npm test` 纳入门禁。

### 6.3 配置和容器检查：`container-check`

仓库应提供只包含假值的 `.env.ci`，并执行：

```bash
docker compose --env-file .env.ci config --quiet
```

CI **MUST** 使用现有 Dockerfile 构建以下 8 个应用镜像：

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

第一版采用固定的完整构建清单，不建设受影响路径矩阵。构建过程：

- **MUST NOT** 登录或推送镜像仓库。
- **MUST NOT** 使用 `latest` 作为验证结果的唯一标识。
- **MUST NOT** 通过 build args 传入密钥。
- **SHOULD** 使用 BuildKit 缓存缩短重复构建时间；缓存失败不得改变检查结果。

Hadolint **SHOULD** 检查根目录和 `web-ui` 的 Dockerfile。

### 6.4 安全检查：`security-check`

CI 保留确定性的基础安全扫描。至少包括：

- Gitleaks：密钥泄漏检查。
- govulncheck：Go 可达漏洞检查。
- gosec：Go 源码安全检查。
- Trivy filesystem：依赖和配置检查。
- Trivy image：构建后镜像检查，可放入 `container-check`。

门禁规则：

| 问题 | 第一版处理 |
|---|---|
| 已确认的密钥泄漏 | 阻塞 |
| Critical 漏洞 | 阻塞 |
| 可达 Go 漏洞 | 阻塞 |
| 存量 High 问题 | 首次接入时形成清单，再决定阻塞日期 |
| Medium/Low | 输出报告，不阻塞 |

安全例外必须记录问题编号、原因、负责人和到期时间。第一版不建立额外的定时全历史扫描工作流。

## 7. 合并门禁与结果

### 7.1 `CI Gate`

工作流 **MUST** 提供名为 `CI Gate` 的最终 Job，并使用 `if: always()` 汇总所有必需 Job。

- 所有必需 Job 成功时，`CI Gate` 成功。
- 任一必需 Job 失败、取消或缺少预期结果时，`CI Gate` 失败。
- 仅由明确条件造成的合法跳过可以接受。

仓库分支保护启用后，应只把稳定的 `CI Gate` 设置为 required status check，降低内部 Job 调整对保护规则的影响。

### 7.2 日志和 Artifact

- GitHub Actions 日志应能定位到失败的 Job、命令或镜像。
- 覆盖率、安全报告或失败日志可按实际排障需要上传。
- 不上传 `node_modules`、Go 缓存、普通二进制、完整镜像 tar、`.env` 或含密钥的配置。
- 第一版不制定 P50/P95、固定保留天数等运营指标；使用 GitHub 默认日志能力即可。

## 8. 本地复现

复杂命令 **SHOULD** 放入 Makefile 或 `.ci/scripts/`，GitHub Actions 只负责环境准备和调度。

建议提供以下入口：

```text
make ci-go
make ci-web
make ci-security
make ci-container
make ci
```

其中 `make ci` 应在具备 Docker、Go、Node.js 的开发环境中执行全部核心检查。脚本失败必须返回非零退出码。

## 9. 实施顺序

### Phase 1：基础门禁

- Go 格式、依赖、vet、测试和 7 个服务编译。
- 前端 `npm ci`、lint 和 build。
- Compose 配置解析。
- `CI Gate`。

### Phase 2：容器与安全

- 构建全部 8 个应用镜像。
- Hadolint。
- Gitleaks、govulncheck、gosec、Trivy。
- 确认并登记存量扫描问题。

### Phase 3：按真实需求增强

仅在项目出现对应需求后评估：

- 前端自动化测试及覆盖率门禁。
- 生成代码漂移检查。
- 完整 Compose 冒烟测试。
- 更细的路径过滤、构建矩阵或测试拆分。

不预先实施定时扫描、Nightly、压力测试和 CI 指标平台。

## 10. 第一版验收标准

第一版 CI 完成时必须满足：

1. PR、`main` 推送及手动操作可以触发 `ci.yml`。
2. Go 检查和依赖服务测试真实执行。
3. 7 个 Go 服务和前端均能成功构建。
4. Compose 配置能够解析，8 个应用镜像均能构建。
5. 基础安全扫描生效，明确的严重问题会阻塞合并。
6. CI 不使用生产密钥，不调用真实生产外部服务，不推送镜像。
7. `CI Gate` 为唯一、稳定的合并门禁结果。
8. 开发者可以通过 `make ci` 或等价脚本在本地复现核心检查。

## 11. 与部署工作的边界

本文件只负责证明提交满足合并条件，不决定应用部署位置和部署工具。

部署应另建技术选型记录，重点比较：

- GitHub Actions + GHCR + 单机 Docker Compose + SSH。
- 自托管 Coolify + Docker Compose。
- Portainer CE 的管理能力及免费版自动部署限制。
- K3s + Argo CD 在多机、高可用场景下的成本与复杂度。

在部署方案确定前，CI 只验证 Dockerfile 能正确构建，不申请推送仓库或访问服务器所需的权限。
