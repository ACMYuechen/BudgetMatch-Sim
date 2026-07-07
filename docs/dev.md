## 目录结构

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

## 开发规范

### 新增模块步骤

1. 在 `desc/` 下创建 `<module>/<module>.api`
2. 在 `desc/app.api` 中 `import` 并添加 `service` 块
3. 运行 `make api-all` 生成代码
4. 在 `internal/logic/<module>/` 下编写业务逻辑

### 2. 目录职责

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

### 3. API 定义规范

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
