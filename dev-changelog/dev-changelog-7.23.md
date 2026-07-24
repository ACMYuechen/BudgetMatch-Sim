# 开发改动日志（dev changelog）

> 用途：每次完成一段编码改动后，把「改了什么、为什么、影响面、怎么验证的」续写到本文档末尾，供组内同步与回溯。
> 约定：按时间顺序**追加在文末**，一次改动一节；标题格式 `YYYY-MM-DD 改动主题（分支名）`。
> 粒度：写"能让没跟进这段工作的同事看懂"的程度即可，行级细节留给 git diff。

## 2026-07-23 logic 层错误日志与 RPC 业务错误透传（main）

### 改了什么

- 在 `cmd/app/internal/logic/**` 和 `cmd/admin/internal/logic/**` 中，把所有 `return err` / `return nil, err` 的错误分支都补上了明确的 `l.Logger.Errorf(...)`，保证每个 error 返回点至少有一条日志。
- 对 API 层调用 RPC 的业务错误分支，不再把 RPC 返回值包装成 `errors.Internal`、`errors.Database` 等统一错误，而是直接原样返回 RPC 的 `err`，避免 HTTP 响应被错误降级成 `500000:internal.default`。
- 保留了本地校验类错误和空结果守卫的显式业务错误返回，例如未登录、资源不存在、RPC 返回空对象等场景仍然使用本地 `infra/errors`。

### 为什么

- logic 层补日志后，error 返回链路更容易定位，避免只看到 handler 层统一报错却找不到具体出错位置。
- RPC 的业务错误本身已经是 `bizerr`，API 层再二次包装会丢失真实错误码和语义，客户端只能看到 `internal.default`，不利于前端和调用方做精确处理。

### 影响面

- app / admin 两个 REST API 服务中，所有直接调用 RPC 的逻辑分支都改为透传 RPC 原始错误。
- 业务错误的 HTTP 状态码和错误码会继续沿用 RPC 返回值，客户端能看到真实的业务错误码。
- 本地参数校验、鉴权失败、资源不存在等错误仍然保持原有业务定义，不受这次改动影响。

### 怎么验证的

- 扫描确认 API logic 中 RPC 错误分支不再返回 `errors.*` 包装值。
- 执行 `GOCACHE=/private/tmp/gocache-budgetmatch go test -run '^$' ./cmd/... ./services/rpc/...`，编译检查通过。
