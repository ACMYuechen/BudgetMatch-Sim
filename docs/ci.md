## 本地 CI

在仓库根目录运行完整的 CI 检查：

```bash
.ci/scripts/ci.sh
```

本机需要安装 Docker、`go.mod` 声明版本的 Go、Node.js 20 和 npm。也可以通过`.ci/scripts/` 目录中的脚本单独运行各项检查。

如果没有设置 `ETCD_HOSTS` 和 `RAG_TEST_PG_DSN`，Go 检查会临时启动 etcd 和 pgvector 容器。
脚本通过 `go list` 构建仓库包依赖图；Pull Request 和普通 push 只对修改包及其反向依赖运行竞态测试，`workflow_dispatch`、Go 依赖定义、CI 执行文件、Proto 或无法可靠映射的变更会回退到全量竞态测试。`go vet ./...` 和 `go build ./...` 始终覆盖整个仓库，因此新增的包和服务无需维护手写列表。

Pull Request 工作流会先运行 `detect-changes.sh`，再根据目标分支到当前提交之间的完整差异选择 Go、Web、安全和容器检查。
修改 `.github/`、`.ci/` 或无法分类的路径，以及手动触发工作流时，会采用安全回退策略并运行完整检查。只修改文档时，会跳过上述四项耗时较长的检查。

容器检查默认构建所有镜像。在本地只检查指定镜像时，可以传入以空格分隔的目标列表：

```bash
CI_IMAGE_TARGETS="mall-rpc app" .ci/scripts/container-check.sh
```

支持的目标包括 `auth-rpc`、`seckill-rpc`、`mall-rpc`、`agent-rpc`、
`payment-rpc`、`app`、`admin` 和 `web-ui`。

在 GitHub Actions 中，准备任务会验证 Compose 和 Dockerfile，并将公共 Go 依赖阶段导出到共享的 Buildx 缓存。
随后，选中的镜像会作为相互独立的 matrix 任务并行运行；每个任务都会读取后端公共缓存，并维护自己的目标构建缓存。

