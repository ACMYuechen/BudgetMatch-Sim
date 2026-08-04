# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把「改了什么、为什么、影响面、怎么验证的」续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序**追加在文末**，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写"能让没跟进这段工作的同事看懂"的程度即可，行级细节留给 git diff。

## 2026-08-04 认证模块：用户名实时校验 + 错误提示优化（feat/auth-check-username）

### 改了什么

**后端新增 CheckUsername 接口：**
- `services/rpc/auth/proto/auth.proto`：新增 `CheckUsernameReq` / `CheckUsernameResp` 消息定义和 `CheckUsername` RPC 方法
- `services/rpc/auth/internal/logic/authservice/check_username_logic.go`：新增 RPC 逻辑，调用 `UserStore.FindByUsername` 查询数据库判断用户名是否存在
- `cmd/app/internal/handler/auth/check_username_handler.go`：新增 HTTP Handler，暴露 `GET /api/auth/check-username?username=xxx`
- `cmd/app/internal/logic/auth/check_username_logic.go`：新增网关 Logic，转调 auth-rpc 的 CheckUsername
- `cmd/app/internal/types/types.go`：新增 `CheckUsernameReq` / `CheckUsernameResp` 请求响应结构体
- `cmd/app/internal/handler/routes.go`：注册 `/api/auth/check-username` 路由
- `services/rpc/auth/internal/interceptor/auth_interceptor.go`：将 `CheckUsername` 加入 `noAuthMethods` 免认证白名单

**前端注册页实时校验：**
- `web-ui/src/api/auth.ts`：新增 `checkUsername(username)` API 封装
- `web-ui/src/pages/RegisterPage.tsx`：发送验证码前先调用 `checkUsername` 校验用户名，已存在则弹出"该用户名已被使用"并阻止发送

**错误提示中文优化：**
- `web-ui/src/pages/LoginPage.tsx`：登录失败时屏蔽 status 码原始错误，按匹配规则显示中文提示（账号不存在/密码错误/认证失败）
- `web-ui/src/pages/RegisterPage.tsx`：注册失败时区分"用户名已被使用"与其他错误，显示对应中文提示
- `web-ui/src/pages/RegisterPage.tsx`：验证码发送失败统一提示"验证码发送失败，请稍后重试"
- `web-ui/src/components/AppLayout.tsx`：退出登录后弹出"已成功退出"提示

### 为什么

1. 原注册流程中用户名查重只在提交注册时才校验，用户体验差——用户填完所有信息后才发现用户名已被占用，需要重新填写。改为点击"获取验证码"时即时校验，可以让用户尽早发现问题。
2. 原前端错误提示直接展示后端返回的英文 status 码（如 `status 503`），对普通用户不友好，需要统一转换为中文友好提示。
3. 退出登录后无任何反馈，用户不确定操作是否成功。

### 影响面

- 认证模块：新增 `CheckUsername` RPC 方法和 HTTP 端点，不影响现有登录/注册/验证码发送逻辑
- auth-rpc 服务：`noAuthMethods` 白名单新增一项，确保注册页未登录用户可调用
- 前端注册页：`handleSendCode` 逻辑变更，发送验证码前必须通过用户名校验
- 前端登录/注册页：错误提示文案变更，从原始错误码改为中文提示
- 前端 AppLayout：退出登录新增 message 提示

### 怎么验证的

- 后端编译通过：`go build ./services/rpc/auth/...` 和 `go build ./cmd/app/...` 均无报错
- 前端构建通过：`npx vite build` 构建成功
- 手动测试场景：
  - 输入已存在的用户名（如 `mislong`）+ 任意邮箱，点击"获取验证码" → 弹出"该用户名已被使用"，不发送验证码
  - 输入不存在的用户名 + 有效邮箱 → 正常发送验证码，显示"验证码已发送"
  - 登录时输入错误密码 → 弹出"认证失败，请检查账号或密码"而非原始 status 码
  - 退出登录 → 弹出"已成功退出"提示
