# Agent 脚本模型容错评测

版本：scripted-react-v1；代码：ad91f2b+M2.2-worktree（调用者提供）；环境：linux / amd64 / go1.26.8。

脚本 SHA-256：`7d99a34214437da85507574a725b9ef3b55191eb98b5eaabff59965f77b7320e`；快照 SHA-256：`2339af22c7d6b6f975b978d74a1cda3d159fd3c423009a15bfd993a4bb58bc70`。

通过 16/16；脚本模型调用 33 次；门禁：true。真实模型/Embedding：not_run；Token/费用未测量。

| 场景 | 结果 | 模型调用 | 工具请求 | 商品查询 | 服务级兜底 | 内部选择兜底 | 重放检查 | 未通过检查 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| normal_tool_flow | completed | 3 | 2 | 1 | 0 | false | true |  |
| no_selection | completed | 1 | 0 | 1 | 0 | true | true |  |
| malformed_json_repaired | completed | 4 | 3 | 1 | 0 | false | true |  |
| negative_limit_repaired | completed | 4 | 3 | 1 | 0 | false | true |  |
| unknown_sku_repaired | completed | 4 | 3 | 1 | 0 | false | true |  |
| inflated_limits_clamped | completed | 3 | 2 | 1 | 0 | false | true |  |
| model_unavailable | completed | 1 | 0 | 1 | 1 | false | true |  |
| model_error_after_selection | completed | 3 | 2 | 2 | 1 | false | true |  |
| tool_transient_repaired | completed | 4 | 3 | 2 | 0 | false | true |  |
| unknown_tool | completed | 1 | 1 | 1 | 1 | false | true |  |
| max_steps | completed | 1 | 1 | 2 | 1 | false | true |  |
| model_permission | error | 1 | 0 | 0 | 0 | false | false |  |
| tool_permission | error | 1 | 1 | 1 | 0 | false | false |  |
| model_deadline | error | 1 | 0 | 0 | 0 | false | false |  |
| cancel_during_model | error | 1 | 0 | 0 | 0 | false | false |  |
| invalid_text_before_model | rejected | 0 | 0 | 0 | 0 | false | false |  |

- 真实 Eino ReAct + 业务工具 + Service + InMemory；模型输出和故障按固定脚本注入，每例隔离，不启动文件/MCP/数据库或外部客户端。
- 工具请求含未知工具等失败尝试，不等于成功执行次数。服务级规则兜底与 Agent 内部未选择兜底分别计数，均不能算模型选品成功。
- 修复脚本必须收到上一步工具反馈；统计跨 WithTools 克隆共享。完成轮次重放校验完整响应和零新增模型/商品/兜底调用与轮次。
- 取消在模型执行中主动触发；超时注入 DeadlineExceeded 错误，不是墙钟截止或真实网络超时测试；MaxStep 使用当前锁定 Eino 的图节点步数，不等于模型调用次数。
- 这是确定性代码容错测试，不是模型自主纠错、语义质量、注入抵抗、生产性能或真实数据库幂等性的证据。
