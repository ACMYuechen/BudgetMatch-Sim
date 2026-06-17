# errors 错误库使用说明

本目录统一维护业务错误码与多语言文案。

## 目录结构

```
infra/errors/
├── errors.go        # 错误码常量 RCxxx 与 AppError 变量定义
├── locale.zh.toml   # 中文错误文案
├── locale.en.toml   # 英文错误文案
└── README.md        # 本文档
```

## 错误码设计规则

- 错误码以 `RC` 开头，类型为 `int64`。
- 错误码共 6 位，**前三位对应 HTTP 状态码**，后三位为业务序号。
- 按 HTTP 状态码分族：
  - `400xxx`：请求参数或用户可修正的输入错误
  - `401xxx`：认证、凭据或令牌错误
  - `404xxx`：资源不存在
  - `409xxx`：请求与当前业务状态冲突
  - `410xxx`：资源曾经存在但已不再可用
  - `429xxx`：限流或滥用防护
  - `500xxx`：系统内部或依赖服务错误

> **重要**：已有错误码的相对顺序不要改动，否则会导致线上错误码不稳定。新增错误码时，在对应分族末尾追加即可。

## 如何添加一个新错误

### 1. 在 `errors.go` 中添加错误码常量

找到对应 HTTP 状态码的 `const` 块，在块末尾新增一个常量：

```go
// 400xxx: 请求参数或用户可修正的输入错误。
const (
	RCInvalid = 400000 + iota
	RCInvalidEmail
	RCCodeInvalid
	RCCodeExpired
	RCSeckillActivityNotStart
	RCMyNewError  // 新增
)
```

命名规范：`RC` + 驼峰描述，例如 `RCInvalidEmail`、`RCMallOrderNotFound`。

### 2. 在 `errors.go` 中添加错误变量

在同组的 `var` 块中新增一行，使用 `newAppError(code, msgId)`：

```go
var (
	Invalid              = newAppError(RCInvalid, "invalid.default")
	InvalidEmail         = newAppError(RCInvalidEmail, "invalid.invalid_email")
	// ...
	MyNewError = newAppError(RCMyNewError, "mall.my_new_error")  // 新增
)
```

- 变量名不要加 `Err` 前缀，直接使用名词，例如 `InvalidEmail`、`MallOrderNotFound`。
- `msgId` 格式为 `section.key`，与 `locale.*.toml` 中的 table key 一一对应。
- 通用错误建议放到标准 section：`invalid`、`unauthorized`、`notfound`、`conflict`、`gone`、`too_many_requests`、`internal`。
- 业务域特定错误可以新增独立 section，例如 `seckill.*`、`mall.*`。

### 3. 在 `locale.zh.toml` 和 `locale.en.toml` 中添加文案

两个文件必须保持 key 完全一致。例如：

`locale.zh.toml`:
```toml
[mall]
my_new_error = "自定义新业务错误"
```

`locale.en.toml`:
```toml
[mall]
my_new_error = "Custom new business error"
```

### 4. 在业务代码中使用

```go
import "budgetmatch-sim/infra/errors"

if err != nil {
    return nil, errors.MyNewError
}
```

## 常用模板

```go
// 400xxx
const (
    RCMyError = 400000 + iota
    // 在已有常量之后追加
)

var (
    MyError = newAppError(RCMyError, "invalid.my_error")
)
```

```toml
# locale.zh.toml
[invalid]
my_error = "我的错误"

# locale.en.toml
[invalid]
my_error = "My error"
```

## 注意事项

1. **不要删除或重排已有错误码**：错误码是前后端契约，一旦发布应尽量保持稳定。
2. **新增错误码必须追加到对应分族末尾**，利用 `iota` 自动递增。
3. **`msgId` 必须在 `locale.zh.toml` 和 `locale.en.toml` 中同时存在**，且 key 完全一致。
4. 变量声明时统一使用 `newAppError(code, msgId)`；需要携带动态数据时使用 `newAppErrorData(code, msgId, data)`。
5. 如需向后端日志传递更多信息，可在业务层通过 `newAppErrorData` 传入 `data`。
