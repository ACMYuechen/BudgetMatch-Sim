# 本地 Docker 数据源

本地配置以 Docker 实际容器、数据卷、账号及连通性为准，`.env` 是核对后的结果，不是判断已有数据源的依据。生产数据源独立，见 [VPS 部署](deployment-vps.md)。

## 2026-09-21 本机配置

当前采用“依赖运行在 Docker、Go 服务运行在宿主机”的地址配置：

| 依赖 | 宿主地址 | 数据与用途 |
| --- | --- | --- |
| PostgreSQL | `127.0.0.1:5432` | 业务库 `budgetmatch-sim`，原 Docker 账号 `root` |
| Redis | `127.0.0.1:6379` | 复用原 `budgetmatch-sim_redis_data` |
| etcd | `127.0.0.1:22379` | 补齐缺失容器，配置保存在 `budgetmatch-sim_etcd_data` |
| RocketMQ NameServer | `127.0.0.1:19876` | 补齐缺失容器，使用已有 5.1.4 镜像 |
| RocketMQ Broker | `127.0.0.1:10911`，VIP 端口 `10909` | 向宿主客户端通告可达的回环地址 |

复用原 `budgetmatch-sim_postgres_data`，原业务库有 1002 个用户。未清库、未重置已有账号密码，也未改用生产数据库。原独立 Agent 保留库（15432 / 15433）不删除、不停止；5432 避免与其冲突。

`.env` 只保留日常运行需要的业务数据连接，常驻独立测试 DSN 已按用户要求删除，模板也不再提供测试库配置。JWT、模型、Embedding 和邮箱等其他原配置不变。`.env` 保持忽略、权限 600；密码和私有备份不进入 Git。

数据库集成测试复用 CI 已有的 `RAG_TEST_PG_DSN`，仅在测试时显式提供可丢弃环境；`scripts/ci/go-check.sh` 未收到该值时会启动自己的临时 pgvector 容器并在结束后清理。直接 `go test` 未配置时跳过数据库用例，不会读取 `.env` 或回退到 `DATABASE_DSN`。商城用例在测试库内使用随机 schema，避免不同测试互相干扰；这不是允许它们连接业务库。`dev-records -env` 现在必须明确提供 `-dsn-key`，默认仍只读。

RPC 配置现在读取 `${DATABASE_DSN}`、`${REDIS_ADDRESS}` 和 `${REDIS_PASSWORD}`，不再忽略 `.env`、固定连到 15432。Compose 中应用容器仍显式使用 `postgres:5432`、`redis:6379`，不是容器内的 `127.0.0.1`。

## 复用镜像与向量扩展

原 Docker 卷对应 PostgreSQL **16.14 Alpine**。没有将它直接挂到 Debian/glibc 镜像，也没有执行 `docker compose down -v`。本机扩展镜像基于原镜像的固定 digest，仅增加 pgvector **0.8.6**；源码归档有 SHA-256 校验，多阶段构建不把编译工具留在运行镜像中。

构建入口：

```sh
docker build -f deploy/images/postgres-local.Dockerfile \
  -t budgetmatch-sim-postgres:16-alpine-vector deploy/images
```

默认 Alpine 源下载慢时，可加 `--build-arg APK_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/alpine`；保留 APK 签名校验。当前 `.env` 的 `POSTGRES_IMAGE` 使用这个本地镜像，`POSTGRES_PORT=5432`。业务库与独立测试库分别启用 `vector`，不调用 Embedding / LLM，不导入测试商品或会话。

`POSTGRES_IMAGE` 的通用默认值仍是 `pgvector/pgvector:pg16`，适合新卷；已有卷必须先确认版本和 libc 再选择镜像。升级 PostgreSQL 主版本或切换 libc 是独立的数据迁移任务，不应通过改镜像加删卷实现。

## 启停与配置边界

在仓库根目录启动或恢复依赖：

```sh
docker compose -p budgetmatch-sim up -d postgres redis etcd rocketmq-namesrv rocketmq-broker
docker compose -p budgetmatch-sim ps
```

这里只管理本项目依赖，不删除其他项目容器、卷或本机 k3s。依赖端口仅绑定 `127.0.0.1`。`make dev` 会加载 `.env`，并为宿主服务设置 etcd、NameServer 和 Broker 地址；注意它原有的端口清理行为，本轮没有执行该脚本，也没有启动整套 Go 服务或调用付费模型。

宿主模式使用 `ROCKETMQ_BROKER_IP1=127.0.0.1`，因为 NameServer 返回的容器网段地址在本机不可达。**全容器运行时应清空该值，并清除宿主专用 `ETCD_ADVERTISE_CLIENT_URLS` 覆写**，让容器使用内部网络地址；不能把宿主模式 `.env` 直接当成完整容器部署配置。

etcd 配置初始化遵循“仅补缺失键”，已有值保留；本轮新增持久卷前已保存并恢复六项 `/config/` 配置。Redis 复用已有持久卷，不执行 `FLUSHALL`。

本轮是数据依赖恢复与配置同步，不是本地 k3s 应用发布。本地 k3s 中此前暂存的生产凭据已清除。以后如在 k3s 内部署应用，应另外验证 Pod 到 Docker 的路由；Pod 的 `127.0.0.1` 不是宿主机。

## 数据保护与验证

原 PostgreSQL、Redis 卷启动前已做冷备份；`.env`、etcd 配置及业务表指纹在本机私有目录 `~/.local/share/budgetmatch-docker-reuse.lbfWH5/` 保留。不要将这些文件提交仓库或公开传输。备份归档不等于已经做过恢复演练。

对业务库只做连接、只读查询及扩展初始化；清理性集成测试只能使用显式选择的可丢弃测试环境，不要求固定库名。最初移除配置时没有删除数据库；随后按用户追加授权执行了下面的定向清理。没有清空现有业务表、迁移此前 Agent 保留库记录，不代表最新业务代码或完整浏览器流程已在本机上线。

实际验证：10 张原业务表的行数及内容指纹在扩展安装前后完全一致，1002 个用户保留；pgvector 0.8.6 的 1024 维读取与距离运算通过。独立 Docker 测试库以非超级用户运行 `product_vectors`、`product_index` 两个 Go 包，82 项测试通过、0 跳过；配置/渲染检查 52 项通过，Compose 实际展开配置与脚本语法检查通过。未向生产库执行这些清理性测试。

### 独立测试库与账号清理

用户要求先提交代码、再删除对应数据库和账号后，已从本地 Docker `127.0.0.1:5432` 删除 `budgetmatch_sim_test` 和 `budgetmatch_test`。执行前核对了原 PostgreSQL 容器、数据卷、集群标识、数据库及角色 OID；测试库内无用户表，账号只拥有该测试库，无其他对象依赖、角色成员关系或活动连接。没有使用强制断连、跨库清理或级联删除角色。

删除后数据库和角色均不存在，其他数据库/角色目录信息及业务库 10 张表的完整内容指纹前后一致，1002 个业务用户保留。`15432` / `15433` 的独立历史实例、Agent 保留库、Redis 和生产环境不在清理范围内。

恢复用备份保存在本机私有目录 `~/.local/share/budgetmatch-testdb-removal.Ncnblw/`：`budgetmatch_sim_test.dump`、`budgetmatch_test-role.sql`、删除前业务表指纹和目录快照。目录权限 `0700`，备份文件 `0600`；角色文件含恢复凭据，只能私密保存，不提交 Git。已检查归档目录可读取，未执行恢复演练；恢复操作说明位于该私有目录的 `RESTORE.md`。此前测试通过的记录为历史验证，不表示测试库仍存在。
