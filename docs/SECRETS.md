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
| `OSS_ENDPOINT` | 阿里云 OSS 接入域名 | 阿里云控制台 → OSS → Bucket 概览 |
| `OSS_ACCESS_KEY_ID` | 阿里云 AccessKey ID | 阿里云 RAM 控制台 → 创建子账号 → 创建 AccessKey |
| `OSS_ACCESS_KEY_SECRET` | 阿里云 AccessKey Secret | 同上，创建时只显示一次 |
| `OSS_BUCKET_NAME` | OSS Bucket 名称 | 阿里云 OSS 控制台 |
| `OSS_DOMAIN` | OSS 自定义域名或外网域名 | Bucket 概览页面 |

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
