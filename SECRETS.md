# 配置与密钥指南

[文档导航](docs/README.md) · [本地数据源](docs/local-data.md) · [生产环境](docs/deployment-vps.md)

变量全集以 [.env.example](.env.example) 和服务 YAML 为准。本页解释配置分组与安全边界，不保存真实密钥或重复记录历史联调过程。

## 建立本地配置

仅在文件不存在时复制模板，随后按已核对的数据源填写：

```bash
[ -f .env ] || cp .env.example .env
chmod 600 .env
```

已有文件应逐项合并模板变化，不整体覆盖。`.env` 被 Git 忽略，但不代表其不会进入终端输出、备份或安全扫描；不要上传完整文件、DSN、Token 或 Secret。

`make dev` 自动加载 `.env`。手动运行服务时，可在已核对文件内容的终端加载：

```bash
set -a
source .env
set +a
```

`source` 会执行 shell 内容，只加载自己维护的可信文件。

## 基础运行配置

| 变量 | 用途与约束 |
| --- | --- |
| `POSTGRES_IMAGE` / `POSTGRES_PORT` | Compose 镜像与映射端口；已有卷先核对 PostgreSQL 版本和 libc |
| `DATABASE_DSN` | 宿主机服务的业务库连接，包含密码；本机复用实例为 5432，模板为 15432 |
| `REDIS_ADDRESS` / `REDIS_PASSWORD` / `REDIS_PORT` | 匹配实际 Docker Redis，不复用生产密码 |
| `JWT_SECRET` | 用户 JWT 签名密钥，使用独立随机值，建议至少 32 字节；各消费方保持一致 |
| `ETCD_HOSTS` / `DEV_ETCD_*` | 服务发现与开发脚本宿主端口，默认开发入口为 22379 |
| `ROCKETMQ_NAMESERVERS` / `DEV_ROCKETMQ_*` / `ROCKETMQ_BROKER_IP1` | 消息服务与 Broker 通告地址，区分宿主机和全容器模式 |
| `EMAIL_FROM` / `EMAIL_PASSWORD` | 邮箱注册、验证码等功能使用的发件邮箱及 SMTP 授权码，不是邮箱登录密码 |
| `PAYMENT_MALL_SERVICE_SECRET` | Payment → Mall 的独立服务身份密钥，至少 32 字节，与用户 JWT 分离；双方一致 |

Compose 应用容器使用内部服务地址，不使用容器内的 `127.0.0.1` 访问宿主服务。细节见[本地连接模式](docs/local-data.md#宿主机与容器模式)。

## 按需启用能力

| 能力 | 配置 | 关闭方式 / 注意事项 |
| --- | --- | --- |
| LLM 推荐 | `LLM_PROVIDER`、`LLM_MODEL`、`LLM_BASE_URL`、`LLM_API_KEY`、`LLM_THINKING` | Provider 留空或 `noop` 走规则；模板默认启用兼容接口，需要自行补齐密钥或关闭 |
| 商品 Embedding | `EMBEDDING_PROVIDER`、`EMBEDDING_MODEL`、`EMBEDDING_BASE_URL`、`EMBEDDING_API_KEY`、`EMBEDDING_DIMENSIONS` | Provider 留空关闭；与 LLM 分别配置，后台同步也可能触发调用 |
| 后台商品索引身份 | `AGENT_MALL_INDEX_SECRET` | 启用 RAG 时必填，至少 32 字节；Agent / Mall 一致且不复用 JWT / 支付密钥 |
| OSS | `OSS_ENDPOINT`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_KEY_SECRET`、`OSS_BUCKET_NAME`、`OSS_DOMAIN` | 按实际使用配置，不将访问密钥暴露给浏览器 |
| 支付宝沙箱 | `ALIPAY_APP_ID`、`ALIPAY_SELLER_ID`、`ALIPAY_PRIVATE_KEY`、`ALIPAY_PUBLIC_KEY`、通知/返回 URL | 未配置时支付服务可启动，但支付功能不可用；完整接入见[支付指南](docs/PAYMENT.md) |

Flash 必须使用 `LLM_MODEL=deepseek-flash` 与 `LLM_THINKING=disabled`；其他模型的 Thinking 留空，不自动转换旧名称。新字段在部署 Secret 引用中可选，是兼容旧环境的措施，**不免除 Flash 的应用校验**。

BGE-M3 使用 `BAAI/bge-m3` 和 1024 维，不能沿用模板的 1536。RAG 还需要 pgvector、Mall 和独立索引身份；模型/维度变化不得自动删表重建。完整示例与索引约束见 [Agent 指南](docs/agent.md#模型与-embedding)。

## 测试与部署边界

普通开发不维护常驻测试 DSN；清理性集成测试由测试入口显式提供 `RAG_TEST_PG_DSN` 等连接，不读取业务 `.env` 或回退 `DATABASE_DSN`，见 [CI 指南](docs/ci.md#go-检查与测试环境)。已删除的旧测试库和账号不需要重新创建。

生产连接由独立 `external-data` Secret 提供，模型/JWT/邮箱等位于 `runtime`，不要把生产密码写回本地业务配置。模板、渲染结果、本机连接通过和生产真实验收是不同层次；更新配置不代表 SMTP、OSS、支付或模型已验证可用。
