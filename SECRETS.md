# 密钥配置指南

项目运行前需要配置以下环境变量。真实密钥保存在项目根目录 `.env` 文件中（已加入 `.gitignore`，不会提交）。

## 必要环境变量

| 变量名 | 说明 | 获取方式 |
|--------|------|---------|
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

2026-09-20 按用户授权已同步本机 `.env` 的 Flash 参数、缺失可选键和独立测试 DSN；文件仍为 `0600` 且不受 Git 跟踪。`BUDGETMATCH_TEST_POSTGRES_DSN` 指向 `127.0.0.1:15432/budgetmatch_sim_test`、独立 `budgetmatch_test` 角色，不使用保留演示记录的 `budgetmatch_agent_dev`。随机口令只留私密配置，真实测试只能在核对目标后显式加载该 DSN；不要把包含删除/建表逻辑的测试指向演示库。模型参数与最小请求、测试库读写通过不等于整套开发环境或所有外部服务均可用：当前 WSL Docker 不可用，SMTP/OSS/支付未做真实操作验证，旧 JWT 密钥也未自动轮换。

## 快速配置

1. 复制模板：
   ```bash
   cp .env.example .env
   ```

2. 编辑 `.env`，填入你的真实密钥。

3. 加载环境变量：
   ```bash
   export $(cat .env | xargs)
   ```
