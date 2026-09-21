# 密钥配置指南

项目运行前需要配置以下环境变量。真实密钥保存在项目根目录 `.env` 文件中（已加入 `.gitignore`，不会提交）。

## 必要环境变量

| 变量名 | 说明 | 获取方式 |
|--------|------|---------|
| `DATABASE_DSN` | 宿主服务的数据库连接，包含密码，不输出到日志 | 核对本机 Docker 实际账号、库名和映射端口后填写 |
| `REDIS_ADDRESS` | 宿主服务的 Redis 地址 | 核对本机 Docker 映射端口；本机为 `127.0.0.1:6379` |
| `REDIS_PASSWORD` | Redis 密码 | 与已验证的本机 Docker 实例一致，不复用生产密码 |
| `JWT_SECRET` | JWT 签名密钥 | 自行生成随机字符串，长度建议 ≥ 32 |
| `EMAIL_FROM` | 发件邮箱 | QQ 邮箱账号 |
| `EMAIL_PASSWORD` | 邮箱 SMTP 授权码 | QQ 邮箱 → 设置 → 账户 → 开启 SMTP 服务 |

## 可选环境变量

| 变量名 | 说明 | 获取方式 |
|--------|------|---------|
| `LLM_PROVIDER` | 推荐模型的兼容接口名；`openai` 启用，留空/`noop` 关闭 | 按目标模型服务填写 |
| `LLM_MODEL` | 模型名；Flash 为 `deepseek-flash`，不自动映射旧名称 | 目标服务的模型列表 |
| `LLM_THINKING` | Flash 必须为 `disabled`；其他模型留空 | 非密钥配置，当前只支持 Flash 非思考模式 |
| `LLM_BASE_URL` | 模型 API 地址，如 `https://api.deepseek.com/v1` | 目标模型服务文档 |
| `LLM_API_KEY` | 启用模型时必填的访问密钥 | 目标服务控制台；不写入代码、日志或清单明文 |
| `OSS_ENDPOINT` | 阿里云 OSS 接入域名 | 阿里云控制台 → OSS → Bucket 概览 |
| `OSS_ACCESS_KEY_ID` | 阿里云 AccessKey ID | 阿里云 RAM 控制台 → 创建子账号 → 创建 AccessKey |
| `OSS_ACCESS_KEY_SECRET` | 阿里云 AccessKey Secret | 同上，创建时只显示一次 |
| `OSS_BUCKET_NAME` | OSS Bucket 名称 | 阿里云 OSS 控制台 |
| `OSS_DOMAIN` | OSS 自定义域名或外网域名 | Bucket 概览页面 |
| `ALIPAY_APP_ID` | 支付宝沙箱应用 AppID | 详见 [docs/PAYMENT.md](docs/PAYMENT.md) |
| `ALIPAY_PRIVATE_KEY` | 应用私钥 | 详见 [docs/PAYMENT.md](docs/PAYMENT.md) |
| `ALIPAY_PUBLIC_KEY` | 支付宝公钥（验签用） | 详见 [docs/PAYMENT.md](docs/PAYMENT.md) |
| `ALIPAY_NOTIFY_URL` | 异步通知地址（公网可达，可留空） | 部署后填网关通知地址 |
| `ALIPAY_RETURN_URL` | 同步跳转地址（当面付可留空） | — |

已有 `.env` 需手动合并模板变更，不要覆盖密钥。Flash 需同时配置 `LLM_MODEL=deepseek-flash` 和 `LLM_THINKING=disabled`；换用其他兼容模型时清空 `LLM_THINKING`。缺失或不支持的 Flash 模式在外部依赖初始化前报错；只清空 `LLM_PROVIDER` 则继续走规则推荐，不要求删除保留的模型配置。部署渲染保留 `LLM_API_KEY` 等原有必需 Secret 引用，仅将新 `LLM_THINKING` 引用标为可选，以兼容未使用 Flash 的旧环境；这不免除 Flash 的应用校验。详见 [Agent 配置迁移](docs/agent.md#1114-m63b-flash-正式配置接入与离线迁移检查)。

历史记录：2026-09-20 同步 Flash 参数时，WSL Docker 当时不可用，独立测试 DSN 使用本机 15432。该地址已被下面的 Docker 配置更新，不再代表当前 `.env`。

2026-09-21 已优先复用原 Docker PostgreSQL / Redis 卷，`.env` 的业务库指向 `127.0.0.1:5432/budgetmatch-sim`。随后按用户要求移除了常驻的独立测试 DSN；日常运行只维护业务连接，不需要配置测试库。真实集成测试统一复用 CI 已有的 `RAG_TEST_PG_DSN`，由 CI/测试入口临时提供，不从 `.env` 或 `DATABASE_DSN` 自动取值；未显式提供时跳过。历史独立测试库和账号未删除，原 1002 个业务用户及此前 Agent 保留库记录不动。数据源、镜像和端口说明见 [本地 Docker 数据源](docs/local-data.md)。不能把包含删表逻辑的测试指向业务库或保留演示库。

文件仍为 `0600` 且不受 Git 跟踪，模型等其他原配置保持不变。本轮数据连接和独立测试通过，不代表整套业务服务已在本地启动，也不代表 SMTP/OSS/支付或新模型已做真实验收；JWT 密钥未自动轮换。生产使用单独的 `external-data` Secret，密码不写入本机业务配置或 Git，详见 [生产部署](docs/deployment-vps.md)。

## 快速配置

1. 仅在配置不存在时复制模板：

   ```bash
   [ -f .env ] || cp .env.example .env
   ```

2. 编辑 `.env`，填入你的真实密钥。

3. 加载自己维护、已核对内容的 `.env`（保留 DSN 中的空格和引号）：

   ```bash
   set -a
   source .env
   set +a
   ```
