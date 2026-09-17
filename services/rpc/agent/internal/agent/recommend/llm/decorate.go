package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxToolPayloadBytes = 256 << 10

// decorate 为工具统一套上「调用记录」与「错误转 JSON」两层装饰。
//
//   - 记录层：把每次工具调用（成功/失败）写入 session.calls，业务工具与 MCP 工具一视同仁；
//   - 错误层：可恢复错误转为 JSON，允许模型修正工具参数；
//     取消、超时和认证错误保留类别并隐藏原始文本，不能被吞掉后继续推理。
//
// name 留空时（如 MCP 工具）由工具自身 Info() 解析。
func decorate(s *session, name string, base tool.BaseTool) tool.BaseTool {
	inv, ok := base.(tool.InvokableTool)
	if !ok {
		return &recordingTool{base: base, name: name, session: s}
	}
	return &recordingTool{base: base, inner: inv, name: name, session: s}
}

// recordingTool 是一层透明装饰器，在调用底层工具前后把结果记录到 session。
type recordingTool struct {
	base    tool.BaseTool
	inner   tool.InvokableTool
	name    string
	session *session
}

// Info 透传底层工具的元信息。
func (t *recordingTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

// InvokableRun 执行底层工具并记录一条工具调用。
func (t *recordingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	started := time.Now()
	var out string
	var err error
	switch {
	case t.inner == nil:
		err = status.Error(codes.PermissionDenied, "unsupported tool interface")
	case len(argumentsInJSON) > maxToolPayloadBytes:
		err = agentcore.ErrInvalidInput
	default:
		out, err = t.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	}
	if stopped := ctx.Err(); stopped != nil {
		err = stopped
	}
	if err == nil && len(out) > maxToolPayloadBytes {
		err = safety.ErrOutputLimit
	}
	if errors.Is(err, os.ErrPermission) {
		err = status.Error(codes.PermissionDenied, "tool access denied")
	}
	name := "tool." + safety.Label(t.resolveName(ctx))
	if err != nil {
		t.session.recordCall(agentcore.ToolCall{Name: name, Success: false, Detail: fmt.Sprintf("error_code=%s duration_ms=%d", safety.ErrorCode(err), time.Since(started).Milliseconds())})
		if agentcore.IsExecutionStopped(err) {
			return "", safety.Protect(err)
		}
		return toolErrorJSON(ctx, err), nil
	}
	t.session.recordCall(agentcore.ToolCall{Name: name, Success: true, Detail: fmt.Sprintf("status=ok output_bytes=%d duration_ms=%d", len(out), time.Since(started).Milliseconds())})
	return out, nil
}

// resolveName 返回工具名，name 为空时从底层工具的 Info 解析。
func (t *recordingTool) resolveName(ctx context.Context) string {
	if t.name != "" {
		return t.name
	}
	if info, err := t.base.Info(ctx); err == nil && info != nil {
		return info.Name
	}
	return "unknown"
}

// toolErrorJSON 把工具错误序列化为 {"success": false, "error": "..."} 反馈给模型。
func toolErrorJSON(_ context.Context, err error) string {
	data, marshalErr := json.Marshal(map[string]any{
		"success": false,
		"error":   safety.ErrorCode(err),
	})
	if marshalErr != nil {
		return `{"success":false,"error":"tool execution failed"}`
	}
	return string(data)
}
