# BudgetMatch-Sim CI 需求规格

> 状态：Draft
>
> 适用分支：`chore/ci`
>
> 最后更新：2026-08-08
>
> 范围：持续集成（CI）

## 1. 文档目的

本文定义 BudgetMatch-Sim 持续集成系统的完整、可验证需求，作为后续编写 GitHub Actions 工作流、CI 脚本、测试配置和安全扫描配置的实施依据。

本文中的关键词含义如下：

- **MUST（必须）**：验收 CI 时不可缺少的要求。
- **SHOULD（应该）**：原则上应实现；若暂不实现，必须记录原因和后续计划。
- **MAY（可以）**：按项目规模、运行时间和免费额度选择实现。

## 2. 项目基线

当前仓库具有以下与 CI 相关的特征：

- 单一 Go Module，`go.mod` 声明 Go `1.26.1`。
- 包含 7 个 Go 可执行服务：
  - `cmd/admin`
  - `cmd/app`
  - `services/rpc/auth`
  - `services/rpc/seckill`
  - `services/rpc/mall`
  - `services/rpc/agent`
  - `services/rpc/payment`
- 包含 1 个 React/Vite 前端：`web-ui`。
- Go 全仓测试中：
  - `infra/dlock` 和 `infra/configcenter` 需要可用的 etcd。
  - pgvector 集成测试需要 `RAG_TEST_PG_DSN`，否则会跳过。
- `web-ui` 当前具备 `lint` 和 `build` 脚本，尚无自动化测试脚本。
- API、RPC、Swagger 和部分 Go 代码通过 `make api-all` 生成。
- 后端服务共用根目录 `Dockerfile`，通过 `SERVICE_PATH` 和 `PORT` 构建参数生成不同镜像。
- 前端使用独立的 `web-ui/Dockerfile`。
- 仓库当前没有项目级 GitHub Actions CI 工作流。

## 3. 目标与非目标

### 3.1 CI 目标

CI 系统必须能够证明：

1. Go 与前端代码符合格式和静态质量要求。
2. 所有自动化测试在确定、隔离的环境中运行并通过。
3. 7 个 Go 服务和前端均能成功构建。
4. API、Proto、Swagger 与生成代码不存在漂移。
5. Dockerfile、Compose 和 CI 配置可以被正确解析。
6. 代码、依赖、配置和容器经过自动化安全扫描。
7. CI 不使用生产密钥，不调用真实生产外部服务。
8. 同一 commit 重跑应产生等价结果。
9. CI 失败能够定位到具体 Job、包、文件或镜像。
10. 开发者能够在本地运行与 CI 等价的检查。

### 3.2 非目标

以下事项不属于本次 CI 建设范围：

- 推送正式镜像或二进制制品。
- 创建 Git Tag、GitHub Release 或版本号。
- 部署到开发、测试、预发布或生产环境。
- 生产环境审批、灰度、回滚和监控。
- 真实支付宝、邮件、OSS、LLM 或 Embedding 调用。
- 生产数据库迁移。

## 4. 工作流总体设计

CI 应至少包含以下工作流：

```text
.github/workflows/
├── ci.yml          # PR 与 main 的核心检查
├── security.yml    # 定时完整安全扫描
└── nightly.yml     # 定时全量集成与稳定性验证
```

核心 `ci.yml` 的逻辑依赖应为：

```text
preflight
   ├── go-quality
   ├── go-test
   ├── web-quality
   ├── generated-code
   ├── config-validation
   └── security-fast
             │
             ▼
       container-verify
             │
             ▼
          ci-gate
```

## 5. 触发与并发需求

### CI-TRG-001：PR 触发

核心 CI **MUST** 在 Pull Request 的以下事件触发：

- `opened`
- `synchronize`
- `reopened`
- `ready_for_review`

### CI-TRG-002：main 触发

核心 CI **MUST** 在代码推送到 `main` 时运行全量检查。

### CI-TRG-003：手动触发

核心 CI、Weekly Security 和 Nightly **MUST** 支持 `workflow_dispatch`，用于基线验证和故障排查。

### CI-TRG-004：定时触发

- `security.yml` **SHOULD** 每周运行一次。
- `nightly.yml` **SHOULD** 在工作日每天运行一次。

### CI-TRG-005：并发控制

同一 PR 的新运行 **MUST** 取消旧运行，避免浪费免费 Actions 配额和产生过期结果。

## 6. 权限与执行安全需求

### CI-SEC-001：最小权限

所有 CI 工作流默认权限 **MUST** 为：

```yaml
permissions:
  contents: read
```

CI **MUST NOT** 请求 `packages: write`、`contents: write` 或部署权限。

### CI-SEC-002：PR 隔离

- PR Job **MUST NOT** 读取仓库或环境中的生产密钥。
- 工作流 **MUST NOT** 使用 `pull_request_target` 执行 PR 中的代码。
- PR 标题、分支名等不可信值 **MUST NOT** 直接拼接进 shell 脚本。

### CI-SEC-003：Action 固定版本

所有第三方及官方 Action **MUST** 固定到完整 commit SHA，并在同一行注释对应版本号。

Action 更新 **SHOULD** 由 Dependabot 管理。

### CI-SEC-004：工具版本

以下工具 **MUST** 固定版本：

- Go
- Node.js
- golangci-lint
- gotestsum
- goctl
- protoc 与 Go 插件
- Gitleaks
- govulncheck
- gosec
- Trivy
- Hadolint

### CI-SEC-005：自建 Runner

如使用自建 Runner：

- 任意 PR 代码 **MUST NOT** 在持久化、高权限 Runner 上运行。
- 自建 Runner **SHOULD** 仅运行可信的 `main` 或定时任务。
- 自建 Runner **SHOULD** 使用一次性或可销毁实例。

## 7. Preflight 需求

### CI-PRE-001：仓库完整性

`preflight` **MUST** 验证以下文件存在：

```text
go.mod
go.sum
Dockerfile
docker-compose.yml
web-ui/package.json
web-ui/package-lock.json
web-ui/Dockerfile
```

### CI-PRE-002：真实环境文件

真实 `.env` **MUST NOT** 被 Git 跟踪；发现后 CI 必须失败。

允许提交：

- `.env.example`
- 只含假数据的 `.env.ci`

### CI-PRE-003：构建矩阵

`preflight` **SHOULD** 输出容器验证矩阵：

- `main` 始终包含全部 8 个镜像。
- PR 根据受影响路径选择镜像。
- `Dockerfile`、`go.mod`、`go.sum`、`infra/**` 改动视为影响全部后端镜像。
- 无法识别影响范围时必须回退到全部镜像，不能静默跳过。

## 8. Go 代码质量需求

### CI-GOQ-001：Go 版本来源

CI **MUST** 以 `go.mod` 为 Go 版本的唯一来源。

### CI-GOQ-002：依赖完整性

CI **MUST** 执行：

```bash
go mod download
go mod verify
go mod tidy
git diff --exit-code -- go.mod go.sum
```

`go mod tidy` 产生差异时必须失败。

### CI-GOQ-003：格式

CI **MUST** 使用 `gofmt -l` 检查全仓 Go 文件。发现未格式化文件时必须打印文件名并失败。

### CI-GOQ-004：静态检查

CI **MUST** 执行：

```bash
go vet ./...
golangci-lint run
```

生成代码的排除规则必须精确，禁止通过排除整个业务目录规避检查。

## 9. Go 测试与编译需求

### CI-GOT-001：测试依赖

`go-test` **MUST** 启动并等待以下服务健康：

- etcd `3.5.15`
- pgvector PostgreSQL 16

### CI-GOT-002：测试环境变量

CI **MUST** 设置：

```text
ETCD_HOSTS=127.0.0.1:2379
RAG_TEST_PG_DSN=<CI 专用 pgvector DSN>
```

CI 必须确保 pgvector 集成测试实际执行，而不是因缺少 DSN 被跳过。

### CI-GOT-003：测试参数

Go 全仓测试 **MUST** 包含：

```text
-count=1
-race
-shuffle=on
-covermode=atomic
-coverprofile=coverage.out
```

### CI-GOT-004：测试报告

测试 **MUST** 生成：

- JUnit XML
- Go coverage profile

即使测试失败，报告也应上传。

### CI-GOT-005：服务编译

测试通过后，CI **MUST** 单独编译 7 个 Go 入口：

```text
cmd/admin
cmd/app
services/rpc/auth
services/rpc/seckill
services/rpc/mall
services/rpc/agent
services/rpc/payment
```

编译 **MUST** 使用 `-trimpath`，输出仅放入临时目录，不作为长期 Artifact 上传。

### CI-GOT-006：覆盖率策略

- 首次稳定运行后 **MUST** 建立覆盖率基线。
- 后续 PR **SHOULD NOT** 降低整体覆盖率。
- 生成代码 **SHOULD** 从覆盖率统计中排除。
- 支付、订单、库存和秒杀模块 **SHOULD** 设置独立阈值。
- 缺陷修复 **MUST** 添加对应回归测试，除非 PR 中说明不可测试原因。

## 10. 前端质量需求

### CI-WEB-001：依赖安装

前端 **MUST** 使用锁文件和：

```bash
npm ci
```

禁止在 CI 使用 `npm install` 更新锁文件。

### CI-WEB-002：Lint 与构建

CI **MUST** 执行：

```bash
npm run lint
npm run build
```

构建必须包含 TypeScript 类型检查。

### CI-WEB-003：前端测试

项目 **SHOULD** 引入 Vitest 与 React Testing Library，并增加：

```bash
npm run test -- --run --coverage
```

优先覆盖：

- 登录、注册和 Token 过期。
- 商品、订单和秒杀核心状态。
- 推荐 SSE 中断与重连。
- 支付状态轮询。
- API 错误统一处理。

### CI-WEB-004：前端报告

前端测试接入后，CI **MUST** 上传 JUnit 与覆盖率报告，但不长期保存 `dist/`。

## 11. 生成代码一致性需求

### CI-GEN-001：工具链固定

在启用严格生成代码检查前，`goctl`、`protoc` 及相关插件版本 **MUST** 被固定并记录。

### CI-GEN-002：漂移检查

CI **MUST** 执行：

```bash
make api-all
git diff --exit-code
```

发现 API、RPC、Swagger 或生成 Go 代码差异时必须失败。

### CI-GEN-003：兼容性检查

项目 **SHOULD** 后续引入 Buf，并检查 Proto lint 与相对 `main` 的破坏性变更。

## 12. 配置验证需求

### CI-CFG-001：CI 环境文件

仓库 **MUST** 提供只含假值的 `.env.ci`。该文件不得包含任何真实邮箱、密钥、Token、支付参数或云账号信息。

### CI-CFG-002：Compose 校验

CI **MUST** 执行：

```bash
docker compose --env-file .env.ci config --quiet
```

### CI-CFG-003：Dockerfile 校验

根目录和 `web-ui` 的 Dockerfile **MUST** 经过 Hadolint 检查。

### CI-CFG-004：外部服务

核心 CI **MUST NOT** 调用真实支付宝、SMTP、OSS、LLM 或 Embedding 服务。相关测试必须使用 Mock、Sandbox stub 或项目已有的确定性降级路径。

## 13. 自动安全扫描需求

### CI-SCAN-001：PR 快速安全扫描

每个 PR **MUST** 执行：

- Gitleaks：新增 commit/diff 密钥扫描。
- govulncheck：Go 可达漏洞扫描。
- gosec：Go 源码安全扫描。
- Trivy filesystem：依赖、Dockerfile 与配置扫描。

### CI-SCAN-002：安全门禁

| 类型 | 门禁要求 |
|---|---|
| 密钥泄漏 | 必须失败 |
| Critical 漏洞 | 必须失败 |
| 可达 Go 漏洞 | 必须失败 |
| High 漏洞 | 存量清理后必须失败 |
| Medium/Low | 生成报告，可暂不阻塞 |

### CI-SCAN-003：例外管理

安全例外 **MUST** 包含：

- 漏洞或规则编号
- 影响范围
- 接受原因
- 负责人
- 创建日期
- 到期日期
- 关联 Issue

例外过期时 CI 必须失败。

### CI-SCAN-004：Weekly Security

每周安全工作流 **SHOULD** 执行：

- Gitleaks 全 Git 历史扫描。
- govulncheck 全仓扫描。
- gosec 全仓扫描。
- Trivy 文件系统扫描。
- 8 个容器的构建与镜像扫描。
- npm 生产和开发依赖扫描。
- 安全例外过期检查。

## 14. 容器验证需求

### CI-IMG-001：镜像集合

CI **MUST** 能验证以下 8 个镜像：

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

### CI-IMG-002：禁止推送

CI 容器验证：

- **MUST NOT** 登录镜像仓库。
- **MUST NOT** 推送镜像。
- **MUST NOT** 使用 `latest`。
- **MUST NOT** 通过 build args 传入密钥。

### CI-IMG-003：构建策略

- PR **SHOULD** 构建受影响镜像。
- `main` **MUST** 构建全部 8 个镜像。
- Docker、Go 依赖或共享基础代码变更 **MUST** 构建全部相关镜像。

### CI-IMG-004：镜像扫描

构建后的本地镜像 **MUST** 使用 Trivy 扫描 High 和 Critical 漏洞。

### CI-IMG-005：缓存

容器构建 **SHOULD** 使用 BuildKit 的 GitHub Actions Cache。缓存失效只能影响速度，不能影响正确性。

## 15. 统一门禁需求

### CI-GATE-001：稳定结果

核心工作流 **MUST** 提供唯一、稳定命名的最终 Job：`CI Gate`。

### CI-GATE-002：始终执行

`CI Gate` **MUST** 使用 `if: always()` 汇总所有依赖 Job。

### CI-GATE-003：Fail Closed

结果判定：

- `success`：通过。
- 因明确路径条件导致的 `skipped`：允许。
- `failure`：失败。
- `cancelled`：失败。
- 缺少预期结果：失败。

CI 无法确认某项检查成功时，不得把总体结果标记为成功。

## 16. Nightly 需求

### CI-NGT-001：重复与并发测试

Nightly **SHOULD** 执行至少 3 次带 race 和 shuffle 的全仓 Go 测试，用于发现顺序依赖和偶发并发问题。

### CI-NGT-002：完整服务集成

Nightly **SHOULD**：

1. 构建并启动完整 Docker Compose。
2. 等待必要服务健康。
3. 执行 `make smoke-test`。
4. 执行有时限的秒杀压力测试。
5. 收集失败服务日志。
6. 在 `always()` 清理容器和 CI 数据卷。

### CI-NGT-003：压力脚本安全

压力测试脚本 **MUST** 具有明确目标地址、并发上限、持续时间和退出码，并防止误指向非 CI 环境。

## 17. 报告与 Artifact 需求

### CI-ART-001：允许上传的 Artifact

CI 可以上传：

- JUnit 报告
- 覆盖率报告
- 安全扫描 JSON/SARIF 报告
- 失败日志
- 必要的 SBOM

### CI-ART-002：禁止上传的 Artifact

CI 不应上传：

- `node_modules`
- Go Module Cache
- 普通编译二进制
- 完整 Docker 镜像 tar
- 正常运行日志
- `.env` 或含密钥的配置

### CI-ART-003：保留期限

- PR 普通报告 **SHOULD** 保留 7 天。
- Weekly/Nightly 失败报告 **SHOULD** 保留 14 天。
- 报告中不得包含密钥原文。

## 18. 缓存与免费额度需求

### CI-CACHE-001：Go

CI **SHOULD** 缓存 Go Module 与 Build Cache，缓存键至少包含 Runner OS、Go 版本和 `go.sum` 哈希。

测试结果不得依赖缓存，必须使用 `-count=1`。

### CI-CACHE-002：Node

CI **SHOULD** 缓存 npm 下载目录，不缓存 `node_modules`。缓存键至少包含 Node 版本和 `package-lock.json` 哈希。

### CI-CACHE-003：资源控制

- 每个 Job **MUST** 设置合理超时。
- 同一 PR 的旧运行 **MUST** 被取消。
- 重型扫描和压力测试 **SHOULD** 放入 Weekly/Nightly。
- 普通 PR 的 P95 运行时间 **SHOULD** 不超过 15 分钟。

## 19. 本地复现需求

### CI-LOCAL-001：仓库脚本

复杂 CI 逻辑 **SHOULD** 放入 `.ci/scripts/`，而不是全部写成 Workflow 内联脚本。

建议目录：

```text
.ci/
├── scripts/
│   ├── check-go-format.sh
│   ├── check-go-mod.sh
│   ├── build-services.sh
│   ├── check-generated.sh
│   ├── validate-config.sh
│   ├── security-fast.sh
│   └── ci-gate.sh
├── tool-versions.env
└── security-exceptions.yaml
```

### CI-LOCAL-002：Makefile 入口

项目 **SHOULD** 提供：

```text
make ci-go-quality
make ci-go-test
make ci-web
make ci-generated
make ci-config
make ci-security
make ci-container
make ci
```

GitHub Actions 负责环境、缓存、并行调度和报告上传；实际检查命令应尽量通过仓库脚本或 Makefile 复用。

## 20. 失败与 Flaky Test 管理

### CI-FLAKE-001：禁止用重试掩盖失败

- 测试失败 **MUST NOT** 通过自动重跑变绿。
- 网络下载可以有限重试。
- 测试本身不得因失败自动重试。

### CI-FLAKE-002：不稳定测试

Flaky Test 必须建立 Issue、负责人和修复期限。确需临时隔离时必须记录原因和恢复日期。

### CI-FLAKE-003：continue-on-error

以下检查在初次引入阶段可以临时设为报告模式：

- golangci-lint 存量问题
- High 漏洞存量
- 初次覆盖率比较

以下检查不得设置 `continue-on-error`：

- 编译
- 自动化测试
- 密钥泄漏
- Critical 漏洞
- `CI Gate`

## 21. 非功能指标

| 指标 | 目标 |
|---|---:|
| PR CI P50 | 不超过 8 分钟 |
| PR CI P95 | 不超过 15 分钟 |
| main 全量 CI | 不超过 25 分钟 |
| Nightly | 不超过 60 分钟 |
| Flaky 失败比例 | 小于 1% |
| 因缓存故障造成的构建失败 | 0 |
| CI 使用生产密钥次数 | 0 |
| 过期安全例外数量 | 0 |

若普通 PR 长期超过 15 分钟，应先分析 Job 时长、缓存命中率和依赖下载，再考虑测试分片或更细路径过滤。

## 22. 分阶段实施要求

### Phase 1：基础门禁

- Go 格式、依赖和 vet。
- Go 测试与 7 个服务编译。
- 前端 lint/build。
- Compose 配置验证。
- `CI Gate`。

### Phase 2：完整质量检查

- golangci-lint。
- Vitest。
- 覆盖率基线。
- 固定生成工具版本。
- 生成代码漂移检查。

### Phase 3：安全门禁

- Gitleaks。
- govulncheck。
- gosec。
- Trivy 文件系统扫描。
- 安全例外管理。

### Phase 4：容器验证

- 受影响镜像矩阵。
- `main` 全 8 镜像构建。
- Trivy 镜像扫描。
- BuildKit 缓存。

### Phase 5：Weekly 与 Nightly

- 全历史安全扫描。
- 完整 Compose 冒烟测试。
- 重复并发测试。
- 秒杀短时压力测试。
- 失败日志归档与自动清理。

## 23. 最终验收标准

CI 实施完成后必须满足：

1. 新 PR 自动触发核心 CI。
2. `main` 始终运行全量核心 CI。
3. etcd 相关测试在 CI 中真实执行。
4. pgvector 集成测试不因缺少 DSN 被跳过。
5. 7 个 Go 服务和前端均能构建。
6. Go race、shuffle 和覆盖率检查有效。
7. 生成代码不一致会导致 CI 失败。
8. Compose 与两个 Dockerfile 均经过验证。
9. 密钥泄漏、Critical 漏洞和可达 Go 漏洞会导致 CI 失败。
10. PR 构建受影响容器，`main` 构建全部 8 个容器。
11. 容器验证过程不登录仓库、不推送镜像、不读取生产密钥。
12. 最终提供唯一稳定的 `CI Gate` 结果。
13. 测试失败时能够获得 JUnit、覆盖率或必要日志。
14. 开发者能够通过 `make ci` 本地复现核心检查。
15. 普通 PR CI 的 P95 运行时间不超过 15 分钟。

## 24. 待实施阶段确认项

以下内容需在实现对应阶段前确定：

- golangci-lint 的具体版本和规则集。
- goctl、protoc 及相关插件的锁定版本。
- 首次覆盖率基线和核心包阈值。
- High 漏洞存量清理期限。
- 容器路径影响矩阵的具体实现方式。
- Weekly/Nightly 的最终 cron 时间。
- GitHub 托管 Runner 免费额度不足时的 Runner 策略。
