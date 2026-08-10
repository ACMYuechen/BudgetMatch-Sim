# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把"改了什么、为什么、影响面、怎么验证的"续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序追加在文末，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写到"能让没跟进这段工作的同事看懂"的程度即可，行级细节留给 git diff。

## 2026-08-07 商城订单状态契约统一 + 前端订单流程修复（fix/mall/lhy）

### 改了什么

**1. 统一订单状态契约（Issue #46 核心）：**

- 新增 `web-ui/src/constants/orderStatus.ts`：定义与后端 `mall.proto` 一致的 `OrderStatus` 枚举（0-7）、`OrderStatusText` 文案映射、`OrderStatusColor` 颜色映射，导出 `getOrderStatusText()` 和 `getOrderStatusColor()` 工具函数
- 修改 `web-ui/src/utils/format.ts`：删除旧的错误状态映射（0=待支付 → 应为 1=待支付），改为从 `orderStatus.ts` 重新导出，保持向后兼容
- 修改 `web-ui/src/components/OrderCard.tsx`：删除本地 `statusColors` 定义，使用 `getOrderStatusColor()` 获取颜色，将 `order.status === 0` 改为 `order.status === OrderStatus.PENDING`
- 修改 `web-ui/src/pages/OrderDetailPage.tsx`：同上，将魔法数字替换为常量引用

**2. 修复订单总计金额显示错误：**

- 修改 `web-ui/src/types/api.ts`：`Order` 类型中删除不存在的 `total_amount` 字段，新增 `original_amount`、`discount_amount`、`pay_amount` 三个字段，与后端 `types.go` 的 `MallOrderResp` 完全对齐
- 修改 `web-ui/src/components/OrderCard.tsx` 和 `web-ui/src/pages/OrderDetailPage.tsx`：`order.total_amount` → `order.pay_amount`

**3. 添加导航栏"我的订单"入口：**

- 修改 `web-ui/src/components/AppLayout.tsx`：在导航菜单中新增"我的订单"项（`/orders`），使用 `ShoppingCartOutlined` 图标

**4. 添加支付宝扫码支付功能：**

- 修改 `web-ui/src/api/mall.ts`：新增 `createPayment(id)` 和 `queryPayment(id)` 两个 API 函数，对接后端 `POST /mall/orders/:id/pay` 和 `GET /mall/orders/:id/pay/query`
- 修改 `web-ui/src/pages/OrderDetailPage.tsx`：待支付订单显示"去支付"按钮，点击后调用 `createPayment` 获取二维码，弹窗展示支付宝二维码（`QRCode` 组件），每 3 秒轮询 `queryPayment` 检查支付状态，支付成功后自动关闭弹窗并刷新订单

**5. 修复商品列表接口参数校验：**

- 修改 `cmd/app/internal/types/types.go`：`MallProductListReq.Keyword` 添加 `optional` 标签，修复前端不传 keyword 时返回 400 的问题

**6. 新增商品种子数据脚本：**

- 新增 `scripts/seed_products.sql`：6 个商品 + 12 个 SKU 的测试数据，覆盖键盘、鼠标、显示器、扩展坞、椅子、耳机品类，价格单位为分（与后端 model 一致）

### 为什么

1. **订单状态契约不一致（P0 bug）**：后端 Proto 定义 `PENDING=1`，前端映射 `0=待支付`，导致待支付订单显示成"已支付"，取消按钮也不出现（判断 `=== 0` 而非 `=== 1`）。这是 Issue #46 的核心问题。
2. **订单金额显示为 ¥0.00**：前端 `Order` 类型使用 `total_amount` 字段，但后端返回的是 `pay_amount`，字段名不匹配导致取不到值。
3. **导航栏缺少订单入口**：用户只能通过手动输入 URL `/orders` 才能访问订单列表，严重影响用户体验。
4. **缺少支付按钮**：后端已实现支付宝扫码支付接口（`CreatePayment`/`QueryPayment`），但前端未对接，用户下单后无法进入支付流程。
5. **商品列表 400 错误**：`Keyword` 字段缺少 `optional` 标签，前端不传该参数时 go-zero 校验器返回 400。
6. **数据库无商品数据**：新增种子数据脚本方便开发和测试。

### 影响面

- **前端**：修改 6 个文件（`orderStatus.ts` 新增、`format.ts`、`OrderCard.tsx`、`OrderDetailPage.tsx`、`AppLayout.tsx`、`api.ts`、`mall.ts`），影响订单列表、订单详情、导航栏、商品列表页
- **后端**：仅修改 `cmd/app/internal/types/types.go` 一个文件（`Keyword` 加 `optional`），需重启 app 网关服务生效
- **数据库**：新增种子数据脚本，不影响已有数据
- **后端测试**：Issue 中提到的 `MallOrderStatusPending` 编译错误在当前代码中已不存在，无需修复

### 怎么验证的

- `npm run build` 构建通过（TypeScript 类型检查 + Vite 打包）
- `npm run lint` ESLint 检查通过（0 warnings）
- 手动验证流程：登录 → 商城页显示商品 → 选 SKU 下单 → 订单详情页显示"待支付"状态 + "去支付"和"取消订单"按钮 → 订单总计金额正确显示 → 点击"去支付"弹出支付宝二维码 → 导航栏"我的订单"入口可正常跳转
- 种子数据通过 `docker exec -i budgetmatch-sim-postgres psql -U root -d budgetmatch-sim < scripts/seed_products.sql` 插入，`SELECT COUNT(*) FROM products` 返回 6

---

## 2026-08-07 认证模块：后端单元测试编写 + 注释完善（feat/auth-backend-tests）

### 改了什么

**新增认证模块后端单元测试，共 19 个测试用例：**

1. 用户名登录测试 — `services/rpc/auth/internal/logic/authservice/username_login_logic_test.go`
   - `TestUsernameLogin_Success`：正确用户名+密码登录成功，验证返回 UserId 和 Token
   - `TestUsernameLogin_UserNotFound`：不存在的用户名返回 `UserNotFound` 错误
   - `TestUsernameLogin_InvalidPassword`：正确用户名+错误密码返回 `InvalidPassword` 错误

2. 邮箱登录测试 — `services/rpc/auth/internal/logic/authservice/email_login_logic_test.go`
   - `TestEmailLogin_Success`：正确邮箱+密码登录成功，验证返回 UserId、Token 和 Role
   - `TestEmailLogin_UserNotFound`：不存在的邮箱返回 `UserNotFound` 错误
   - `TestEmailLogin_InvalidPassword`：正确邮箱+错误密码返回 `InvalidPassword` 错误
   - `TestEmailLogin_InvalidEmailFormat`：空邮箱（格式由前端校验）返回 `UserNotFound` 错误

3. 邮箱注册测试 — `services/rpc/auth/internal/logic/authservice/email_register_logic_test.go`
   - `TestEmailRegister_Success`：正确邮箱+密码+用户名+验证码注册成功
   - `TestEmailRegister_InvalidEmailFormat`：无效邮箱格式返回 `InvalidEmail` 错误
   - `TestEmailRegister_UsernameAlreadyExists`：已注册用户名返回 `UserExists` 错误
   - `TestEmailRegister_EmailAlreadyExists`：已注册邮箱返回 `UserExists` 错误
   - `TestEmailRegister_CodeExpired`：验证码不存在/已过期返回 `CodeExpired` 错误
   - `TestEmailRegister_CodeInvalid`：验证码不匹配返回 `CodeInvalid` 错误
   - `TestEmailRegister_CodeConsumed`：验证码使用后立即删除，防止重复注册

4. 发送验证码测试 — `services/rpc/auth/internal/logic/authservice/send_code_logic_test.go`
   - `TestSendCode_InvalidEmail`：无效邮箱格式返回 `InvalidEmail` 错误
   - `TestSendCode_EmptyEmail`：空邮箱返回 `InvalidEmail` 错误
   - `TestSendCode_RateLimitExceeded`：60 秒内重复发送返回 `TooManyRequests` 错误
   - `TestSendCode_RateLimitWindow`：验证限流窗口常量 `RateLimitWindow` 为 60 秒
   - `TestSendCode_CodeExpireTime`：验证验证码过期时间常量 `CodeExpireTime` 为 300 秒

**为所有测试函数添加中文注释：**
- 每个测试函数前添加 `// TestXxx 测试XX场景` 格式的注释
- 补充第二行说明验证的具体行为和预期结果
- 提高测试代码可读性，方便团队成员理解每个用例的意图

### 为什么

1. 认证模块是系统安全入口，登录/注册/验证码逻辑的正确性直接影响用户体验和安全性，需要系统性的单元测试覆盖正常流程和各类边界异常。
2. 之前认证模块缺少自动化测试，改动后只能靠手动验证，回归风险高。补充单元测试后可以在后续重构时快速确认核心逻辑未被破坏。
3. 测试函数仅靠函数名难以完整表达测试意图（如 `CodeConsumed` 需要说明"验证码使用后立即删除防重复"），添加注释后可读性显著提升。

### 影响面

- 仅新增测试文件，不修改任何业务代码逻辑，对现有服务运行零影响
- 测试使用 Mock 对象（`mockUsersModel` / `mockEmailUserModel` / `mockRegisterUserModel`）替代真实数据库，使用 `miniredis` 替代真实 Redis，不依赖 Docker 容器或 `make dev` 启动
- 测试可直接通过 `go test ./internal/logic/authservice/ -v` 运行，无需启动基础设施

### 怎么验证的

- 测试基于依赖注入设计：通过手动构造 `svc.ServiceContext`，将 Mock 的 `UserStore` 和 `miniredis` 客户端注入 Logic 层，Logic 层代码无感知，保证测试与生产行为一致
- 覆盖场景包括：正常流程（登录成功/注册成功）、用户不存在、密码错误、用户名/邮箱重复、验证码过期/错误/重复使用、限流、邮箱格式校验、常量值校验
- 运行方式：在 `services/rpc/auth` 目录下执行 `go test ./internal/logic/authservice/ -v` 即可，无需启动项目
