package llm

import (
	"context"
	"encoding/json"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// detailLimit 限制工具调用详情记录的长度，避免把整段 JSON 灌进响应。
const detailLimit = 512

// decorate 为工具统一套上「调用记录」与「错误转 JSON」两层装饰。
//
//   - 记录层：把每次工具调用（成功/失败）写入 session.calls，业务工具与 MCP 工具一视同仁；
//   - 错误层：utils.WrapToolWithErrorHandler 把工具错误转成 JSON 反馈给模型，
//     让 ReAct 能据此重试或降级，而不是让单个工具失败中断整个推理链。
//
// name 留空时（如 MCP 工具）由工具自身 Info() 解析。
func decorate(s *session, name string, base tool.BaseTool) tool.BaseTool {
	inv, ok := base.(tool.InvokableTool)
	if !ok {
		return base
	}
	return utils.WrapToolWithErrorHandler(&recordingTool{inner: inv, name: name, session: s}, toolErrorJSON)
}

// recordingTool 是一层透明装饰器，在调用底层工具前后把结果记录到 session。
type recordingTool struct {
	inner   tool.InvokableTool
	name    string
	session *session
}

// Info 透传底层工具的元信息。
func (t *recordingTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

// InvokableRun 执行底层工具并记录一条工具调用。
func (t *recordingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	out, err := t.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	name := "tool." + t.resolveName(ctx)
	if err != nil {
		t.session.recordCall(agentcore.ToolCall{Name: name, Success: false, Detail: err.Error()})
		return out, err
	}
	t.session.recordCall(agentcore.ToolCall{Name: name, Success: true, Detail: truncate(out)})
	return out, nil
}

// resolveName 返回工具名，name 为空时从底层工具的 Info 解析。
func (t *recordingTool) resolveName(ctx context.Context) string {
	if t.name != "" {
		return t.name
	}
	if info, err := t.inner.Info(ctx); err == nil && info != nil {
		return info.Name
	}
	return "unknown"
}

// toolErrorJSON 把工具错误序列化为 {"success": false, "error": "..."} 反馈给模型。
func toolErrorJSON(_ context.Context, err error) string {
	data, marshalErr := json.Marshal(map[string]any{
		"success": false,
		"error":   err.Error(),
	})
	if marshalErr != nil {
		return `{"success":false,"error":"tool execution failed"}`
	}
	return string(data)
}

// truncate 截断过长的工具输出，避免详情记录膨胀。
func truncate(out string) string {
	if out == "" {
		return "completed"
	}
	if len(out) > detailLimit {
		return out[:detailLimit] + "...(truncated)"
	}
	return out
}
