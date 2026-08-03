# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把“改了什么、为什么、影响面、怎么验证的”续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序追加在文末，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。

## 2026-08-03 修复请求上下文解析函数缺失导致编译失败（fix/request/xmy）

### 改了什么

- 恢复 `infra/request` 的 `grpc.go` / `http.go`：`FromHTTPRequest`（HTTP 请求解析）、`FromGRPCContext`（gRPC incoming metadata 解析）、`NewOutgoingContext`（outgoing metadata 透传）。
- 恢复 `infra/errors` 的 `RequestNilHTTP` 错误值（`ECRequestNilHTTP` 常量仍存在），修正 `request` 包过期注释。
- 补充三个函数的单元测试：Bearer 解析（大小写不敏感）、重复头拒绝、nil 请求、`request_id`/`user_agent` 注入、peer/X-Forwarded-For 客户端 IP、outgoing metadata 保留与同键覆盖。

### 为什么

- PR #49（feat/request/hfs）重构 request 包时误删上述函数，但 `cmd/app`、`cmd/admin` 认证中间件与 `infra/interceptor` 的 gRPC 拦截器仍依赖，导致全项目编译失败，注册/登录接口无法启动。

### 影响面

- 仅补回被删代码与测试，不改变接口、业务错误码和角色规则。
- 涉及 `cmd/app`、`cmd/admin` 认证中间件、`infra/interceptor` 通用拦截器、`services/rpc/auth` 认证拦截器的编译恢复。

### 怎么验证的

- `go build ./...`、`go vet ./...`、`go test ./...`（仅 `infra/configcenter`、`infra/dlock` 因缺真实 etcd 失败，与本次改动无关）、`git diff --check` 均通过。
- 本地启动后验证：发送验证码 → 注册返回 `{"success":true}` → 邮箱/用户名登录返回 JWT，错误密码返回 `401001`。
