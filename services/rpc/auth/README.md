# services/rpc/auth — 认证中心

提供用户认证、用户管理的 gRPC + HTTP Gateway 服务。

## 端口

| 协议 | 端口 | 说明 |
|------|------|------|
| gRPC | 10003 | 内部服务间调用 |
| HTTP Gateway | 10000 | 外部 HTTP 直接访问（基于 protobuf http option） |

## 服务列表

### AuthService — 认证相关

| RPC | HTTP 路由 | 说明 | 认证 |
|-----|-----------|------|------|
| UsernameLogin | POST `/api/auth/login/username` | 用户名登录 | ❌ |
| EmailLogin | POST `/api/auth/login/email` | 邮箱登录 | ❌ |
| EmailRegister | POST `/api/auth/register/email` | 邮箱注册 | ❌ |
| LoginByCode | POST `/api/auth/login/code` | 验证码登录 | ❌ |
| SendCode | POST `/api/auth/send_code` | 发送验证码 | ❌ |
| ValidateToken | POST `/api/auth/validate` | 验证 Token | ❌ |

### UserService — 用户信息

| RPC | HTTP 路由 | 说明 | 认证 |
|-----|-----------|------|------|
| GetUserInfo | GET `/api/user/info` | 获取用户信息 | ✅ |
| GetUserProfile | GET `/api/user/profile` | 获取个人资料 | ✅ |
| UpdateUserProfile | POST `/api/user/profile` | 更新个人资料 | ✅ |

## Gateway 模式

auth-rpc 同时暴露 gRPC 和 HTTP 接口：

- **gRPC**: 供内部服务（cmd/app, cmd/admin）通过 `AuthRpc` 客户端调用
- **HTTP Gateway**: 测试时可直接访问，无需额外写客户端调用 gRPC

```go
// main.go 中同时启动 RPC 和 Gateway
s := zrpc.MustNewServer(...)    // gRPC
sg.Add(s)

gw := gateway.MustNewServer(...) // HTTP Gateway
sg.Add(gw)
```

## 认证流程

```
1. 登录/注册 → AuthService 签发 JWT Token
2. 受保护接口声明 middleware: AuthMiddleware
3. 中间件通过 RPC 调用 ValidateToken 验证
4. 将 user_id 注入 context
5. Logic 通过 ctx.Value("user_id") 获取当前用户
```
