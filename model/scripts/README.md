# scripts/ — 开发脚本

辅助开发的脚本工具。

## 脚本列表

| 脚本 | 说明 | 用法 |
|------|------|------|
| `dev.sh` | 一键启动所有服务（本地 go run + Docker 基础设施） | `make dev` |
| `dev-stop.sh` | 一键停止所有本地服务 | `make dev-stop` |

## 服务启动顺序

```
dev.sh 按以下顺序串行启动：

1. docker compose up postgres redis    # 基础设施
2. auth-rpc (port 10003)               # 先启动，创建数据表
3. app (port 10002)                    # 依赖 auth-rpc
4. admin (port 10001)                  # 依赖 auth-rpc
```

> **串行原因**: 预防共享 model 竞争。

## 日志

```
logs/
├── auth-rpc.log
├── app.log
└── admin.log
```

查看实时日志：
```bash
tail -f logs/app.log
```