# 本地数据源

[文档导航](README.md) · [配置指南](SECRETS.md) · [恢复与清理记录](archive/local-data-2026-09.md)

原则：**先核对 Docker 中已有的容器、卷、账号和端口，再填写 `.env`**。配置文件是核对结果，不是删除或重建已有数据的理由。本文记录 2026-09-21 整理时的已知配置，不保证容器此刻正在运行。

## 本机复用配置

| 依赖 | 宿主机地址 | 数据范围 |
| --- | --- | --- |
| PostgreSQL | `127.0.0.1:5432` | 业务库 `budgetmatch-sim`，原账号 `root`，卷 `budgetmatch-sim_postgres_data` |
| Redis | `127.0.0.1:6379` | 原卷 `budgetmatch-sim_redis_data` |
| etcd | `127.0.0.1:22379` | 卷 `budgetmatch-sim_etcd_data` |
| RocketMQ NameServer | `127.0.0.1:19876` | 本地开发消息服务 |
| RocketMQ Broker | `127.0.0.1:10911`，VIP `10909` | 须向宿主客户端通告可达地址 |

`.env.example` 为新环境保留 PostgreSQL 默认端口 **15432**；本机的 15432 / 15433 另有历史 Agent 保留实例，不要覆盖、停止或用于清理性测试。生产数据源独立，见 [VPS 运维](https://github.com/ACMYuechen/BudgetMatch-Sim-Gitops#部署流程)。

密码只保存在被 Git 忽略的 `.env` 或私密配置中。不要输出完整 DSN、`docker inspect` 的全部环境变量或展开后的 Secret 来排查连接。

## 启动前检查

以下只查看容器、卷与端口，不修改数据：

```bash
docker compose -p budgetmatch-sim ps -a
docker volume ls --filter name=budgetmatch-sim
ss -ltn '( sport = :5432 or sport = :15432 or sport = :15433 or sport = :6379 )'
```

确认 `.env` 的 `POSTGRES_PORT`、`POSTGRES_IMAGE`、`DATABASE_DSN`、`REDIS_ADDRESS` 和密码与实际实例一致，再按[首页](../README.md#本地快速开始)使用 `make dev`。仅修改 `.env` 中的密码不会自动修改已有数据库卷里的账号密码。

## 宿主机与容器模式

| 运行方式 | 应用连接方式 | 注意事项 |
| --- | --- | --- |
| `make dev` | Go 服务在宿主机，依赖在 Docker，使用回环地址及映射端口 | 脚本加载 `.env`，设置宿主可达的 etcd / Broker 通告地址，并清理开发端口进程 |
| 手动启动依赖 | 宿主服务仍使用映射端口 | 必须自行核对 `ETCD_ADVERTISE_CLIENT_URLS` 和 `ROCKETMQ_BROKER_IP1`，不能假定具有开发脚本的覆盖设置 |
| 全容器 Compose | 应用使用 `postgres:5432`、`redis:6379` 等服务名 | 清除仅供宿主模式使用的回环通告覆盖；容器中的 `127.0.0.1` 不是宿主机 |
| 本地 K3s | 以 Pod 的服务发现和 Secret 为准 | Docker 数据源配置正确，不等于本地 K3s 已部署或验证最新应用 |

`make dev-stop` 停止开发服务及 Compose 基础设施，不删除卷。不要执行带卷删除选项的清理来解决连接失败，也不要把文档中的历史运行状态当作停止其他实例的依据。

## PostgreSQL 镜像与向量扩展

原数据卷使用 PostgreSQL 16.14 Alpine。本机使用 [postgres-local.Dockerfile](../docker/postgres-local.Dockerfile) 在固定基础镜像上增加 pgvector 0.8.6，镜像名为 `budgetmatch-sim-postgres:16-alpine-vector`。

需要重建本机镜像时，从仓库根目录执行：

```bash
docker build -f docker/postgres-local.Dockerfile \
  -t budgetmatch-sim-postgres:16-alpine-vector docker
```

模板的 `pgvector/pgvector:pg16` 面向新建兼容卷。不要将现有 Alpine 卷直接挂到 Debian/glibc 镜像；更换 libc 或 PostgreSQL 大版本须先备份并单独迁移。安装 `vector` 扩展不等于启用 Embedding，后者可能触发外部调用，见 [Agent 配置](AGENT.md#模型与-embedding)。

## 测试与业务数据隔离

- 普通开发只维护业务连接，不需要在 `.env` 里配置常驻测试 DSN。
- PostgreSQL 集成测试显式使用 `RAG_TEST_PG_DSN`；Agent 记忆测试可用专门的 `AGENT_MEMORY_TEST_PG_DSN`。它们不自动加载 `.env`，也不回退 `DATABASE_DSN`。
- 直接 `go test` 未提供测试连接时，部分数据库用例会跳过；`scripts/ci/go-check.sh` 可准备自有临时 pgvector / etcd 环境并清理。详见 [CI 指南](CONTRIBUTION.md#go-检查与测试环境)。
- 随机 schema 等隔离措施用于可丢弃测试环境，不授权测试连接业务库或保留库。
- 需要留下演示历史时使用 `dev-records` 显式入口；默认只读，`-env` 必须搭配 `-dsn-key`，写入另需确认目标库和已有账号。

弃用的 Docker 测试库 `budgetmatch_sim_test` 与账号 `budgetmatch_test` 已按授权备份后删除；5432 业务数据及其他实例不在删除范围内。私密备份位置、数据指纹与恢复说明索引保留在[维护归档](archive/local-data-2026-09.md#独立测试库与账号清理)，未做恢复演练，不把“备份可读取”当作“恢复已验证”。
