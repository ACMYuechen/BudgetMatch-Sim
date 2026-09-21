# BudgetMatch-Sim

面向电商组合决策的智能推荐系统原型：提供认证、商城、秒杀、支付和推荐 Agent，结合预算、价格、库存与用户偏好生成购物组合。

技术栈：Go / go-zero / gRPC、React / Vite / Ant Design、Eino ReAct、PostgreSQL / pgvector、Redis、etcd、RocketMQ；支持 Docker Compose 与 K3s 部署。

## 从这里开始

| 你想做什么 | 阅读入口 |
| --- | --- |
| 找文档、了解阅读顺序 | [文档导航](docs/README.md) |
| 看完成范围和剩余事项 | [项目状态](docs/status.md) |
| 了解服务、端口和代码分层 | [架构与代码地图](docs/architecture.md) |
| 配置数据库、密钥与模型 | [配置指南](docs/SECRETS.md) · [本地数据源](docs/local-data.md) |
| 开发推荐或前端功能 | [Agent 指南](docs/AGENT.md) · [前端指南](docs/frontend.md) |
| 运行代码检查 | [开发与测试](docs/CONTRIBUTION.md#go-检查与测试环境) |
| 生产部署与运维 | [BudgetMatch-Sim-Gitops（私有仓库）](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops) |
| 查看权限边界和已知风险 | [权限与安全](docs/access-control.md) |

本地开发与自动化验证已有完整链路，但这不等于生产验收完成。已知 RPC 权限缺口、部署版本差异和人工复核事项见[项目状态](docs/status.md)。

## 本地快速开始

### 1. 准备依赖

- Go 版本与 [go.mod](go.mod) 一致，当前为 1.26.8。
- 可用的 Docker daemon 和 Docker Compose。
- Node.js 20 与 npm，用于前端开发。
- 仅在生成代码时需要 goctl，见[开发规范](docs/CONTRIBUTION.md#工具链与依赖版本)。

### 2. 配置环境

```bash
[ -f .env ] || cp .env.example .env
chmod 600 .env
```

编辑 `.env`，核对数据库、Redis、JWT 等参数，详见[配置指南](docs/SECRETS.md)。不要用模板覆盖已有密钥，也不要将生产密码复制到本地开发配置。

已有 Docker 数据时先看[本地数据源](docs/local-data.md)：本机复用实例的 PostgreSQL 端口为 **5432**，模板面向新环境的默认值为 **15432**，不能直接混用。不要删除数据卷来解决连接问题。

仅体验规则推荐时，将 `LLM_PROVIDER` 与 `EMBEDDING_PROVIDER` 留空。模板默认的 `LLM_PROVIDER=openai` 需要有效模型配置；启用 Embedding 后，后台索引同步也可能产生外部调用。

### 3. 启动后端

```bash
make dev
```

该命令加载 `.env`，启动基础设施和七个后端服务；不启动前端。脚本会清理开发端口上的旧进程，执行前确认没有其他项目占用这些端口。日志位于 `logs/`。

### 4. 启动前端

另开终端：

```bash
npm --prefix web-ui ci  # 首次使用或依赖更新后
make web
```

用户端：`http://localhost:5173`；管理端：`http://localhost:5173/admin`，需要管理员账号。Vite 将 `/api/admin` 代理到 `10001`，其余 `/api` 代理到 `10002`。

### 5. 检查与停止

在仓库根目录执行：

```bash
make smoke-test
make dev-stop
```

健康检查只检查两个 API 的 HTTP 接口，不代替完整业务验收。前台 Vite 使用 `Ctrl+C` 停止；`make dev-stop` 会停止后端及 Compose 基础设施，不删除数据卷。

## 常用命令

Makefile 只保留 8 个本地入口：

| 命令 | 用途 |
| --- | --- |
| `make help` | 查看命令 |
| `make dev` / `make dev-stop` | 启动 / 停止后端和本机 Docker 依赖，保留数据卷 |
| `make web` | 前台启动前端，Ctrl+C 停止 |
| `make smoke-test` | 检查两个 API 健康接口，失败返回非零 |
| `make test` | Go 测试；不加载 `.env`，集成测试需独立测试环境 |
| `make api-all` | 生成 API/RPC/Swagger，执行后检查差异 |
| `make ci` | 完整本地 CI，细项直接运行 [CI 脚本](docs/CONTRIBUTION.md#go-检查与测试环境) |

后端日志直接查看 `logs/`，例如 `tail -f logs/app.log`。前端安装和构建使用 npm，容器状态使用 `docker compose ps`；开发终端应连接本机 Docker。生产镜像、GitOps、Argo CD 配置及运维记录统一维护在[私有运维仓库](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops)，生产部署只允许 Argo CD 手动 Sync。

接口定义以 [App API](cmd/app/desc/app.api)、[Admin API](cmd/admin/desc/admin.api) 和各服务的 `.proto` 为准。生成的 Swagger JSON 不受 Git 跟踪，干净检出中不保证存在。

协作开发请先阅读 [开发规范](docs/CONTRIBUTION.md)。阶段日志、旧环境和详细验收证据统一存放在[历史归档](docs/archive/README.md)，不再作为快速开始步骤。
