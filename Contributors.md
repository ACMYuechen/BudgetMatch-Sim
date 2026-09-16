# 开发者文档

## 一、提交格式

### 模板

```Plain Text
<type>(<scope>): <description>

feat(user): 新增手机号登录接口
fix(order): 修复订单支付状态同步异常
refactor(pay): 下线废弃的旧支付逻辑
style: 统一全局代码格式
docs: 更新项目部署文档
test: 移除失效的用户模块测试用例
chore: 优化Dockerfile构建配置
```

## 二、提交类型

所有代码改动必须严格归类，无特殊例外：

- **feat**：新增业务功能、接口、页面、模块能力

- **fix**：修复 Bug、逻辑异常、数据错误、线上隐患

- **refactor**：代码重构、结构优化、逻辑梳理，**无新增功能、无Bug修复**（**核心：所有删除/废弃业务代码均归此类**）

- **style**：代码缩进、空格、格式调整，不修改任何业务逻辑

- **docs**：文档、注释、README、说明文本新增/修改/删除

- **test**：测试用例、测试脚本的新增、修改、删除

- **chore**：工程配置、依赖、Docker、CI/CD、构建脚本等非业务改动

## 三、开发规范

1. 先从 main 拉取最新状态
2. 从 main 创建一条新分支并 check out
3. 分支命名规范 **类型**/**模块名**/(**个人缩写**可选)，(如:refactor/errors/xmy, feat/etcd, dev/agent)
4. **类型**标签与提交标签一致，如果不知道选什么可以直接用dev做标签
5. 功能写好后去 github 提交合并到 main 分支的 pr
6. 审核员(QQ群管理) review 后会对 pr 合并或关闭，若被关闭则会向开发者说明原因
7. pr 被拒绝后重新修改后方可继续申请 pr
8. pr 被成功合并后可自行删除开发分支

## 四、开发流程

### 工具链与依赖版本

- Go 版本以 [`go.mod`](go.mod) 的 `go` 指令为准，当前为 **1.26.8**。
- gRPC 版本由 `go.mod` / `go.sum` 锁定，当前 `google.golang.org/grpc` 为 **v1.83.2**。升级时需一并保留 `go mod tidy` 解析出的必要依赖变更。
- 后端 [`Dockerfile`](Dockerfile) 使用 `golang:1.26.8-alpine`，与项目工具链补丁版本一致；GitHub Actions 的 Go 检查和安全检查通过 `go-version-file: go.mod` 读取版本。
- 前端使用 Node.js 20 和 npm，与当前工作流及 `web-ui/Dockerfile` 一致；用 `npm ci` 按锁文件安装。

在仓库根目录确认实际工具链并下载、校验依赖：

```bash
go version
go env GOTOOLCHAIN
go mod download
go mod verify
```

支持自动工具链切换且 `GOTOOLCHAIN=auto` 时，Go 可按 `go.mod` 下载并使用所需版本；若关闭自动切换或网络受限，需要自行安装匹配版本。这里更新的是项目工具链要求，不代表系统中所有 Go 安装都已替换。

后续升级 Go 时，同步核对 `go.mod`、根目录 Dockerfile、README 与项目说明中的版本；更新依赖后执行 `go mod tidy`、漏洞扫描和回归测试。完整检查入口与环境要求见 [CI 说明](docs/ci.md)，服务及前端启动步骤见 [README](README.md#快速开始)。

### 测试环境

`make test` 仅执行 `go test -v ./...`，不会启动基础设施，也不会自动加载 `.env`。配置中心和分布式锁测试需要显式提供测试用 `ETCD_HOSTS`；PostgreSQL / pgvector 测试变量及跳过条件见 [CI 说明](docs/ci.md#go-检查与测试环境)。测试可能建表、删表或改写键值，必须使用可丢弃的独立测试环境。

### 目录结构

```
cmd/
├── app/                        # 应用网关（面向客户端）
│   ├── desc/
│   │   ├── app.api             # 路由汇总入口
│   │   └── <module>/           # 各模块 API 定义
│   │       └── <module>.api    # 模块请求/响应结构体 + 路由定义
│   ├── etc/
│   │   └── config.yaml         # 服务配置
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go       # 配置结构体（映射 config.yaml）
│   │   ├── handler/
│   │   │   ├── routes.go       # 路由注册（goctl 生成，不要修改）
│   │   │   └── <module>/
│   │   │       └── xxx_handler.go  # HTTP 处理器（goctl 生成，不要修改）
│   │   ├── logic/
│   │   │   └── <module>/
│   │   │       └── xxx_logic.go    # 业务逻辑（手写，安全编辑）
│   │   ├── middleware/
│   │   │   └── auth_middleware.go  # 认证中间件
│   │   ├── svc/
│   │   │   └── service_context.go  # 依赖注入容器（安全编辑）
│   │   └── types/
│   │       └── types.go        # 请求/响应结构体（goctl 生成，不要修改）
│   └── main.go                 # 服务入口
│
└── admin/                      # 管理后台（面向运营）
    └── ...                     # 与 app 相同结构
```

### 新增模块步骤

1. 在 `desc/` 下创建 `<module>/<module>.api`
2. 在 `desc/app.api` 中 `import` 并添加 `service` 块
3. 运行 `make api-all` 生成代码
4. 在 `internal/logic/<module>/` 下编写业务逻辑

### 目录职责

| 目录 | 职责 | 编辑权限 |
|------|------|----------|
| `desc/` | API 接口定义（syntax = "v1"） | ✅ 安全编辑 |
| `etc/` | 服务配置文件 | ✅ 安全编辑 |
| `internal/config/` | 配置结构体映射 | ✅ 安全编辑 |
| `internal/handler/` | HTTP 请求处理器 | ❌ goctl 生成 |
| `internal/logic/` | 业务逻辑 | ✅ **手写，核心开发区** |
| `internal/middleware/` | HTTP 中间件 | ✅ 安全编辑 |
| `internal/svc/` | 依赖注入容器 | ✅ 安全编辑 |
| `internal/types/` | 请求/响应类型 | ❌ goctl 生成 |

### API 定义规范

```go
syntax = "v1"

import (
    "<module>/<module>.api"   // 引入模块定义
)

// 每个模块独立 server 块，便于分组和中间件控制
@server (
    prefix: /api
    group:  <module>
    tags:   <module>
    middleware: AuthMiddleware    // 需要认证时声明
)
service app/admin {
    @doc "接口说明"
    @handler HandlerName          // handler 名称
    get|post|put|delete /path (Req) returns (Resp)
}
```
