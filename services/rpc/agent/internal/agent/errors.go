package agent

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrAgentNotFound 表示未找到指定名称的 Agent。
var ErrAgentNotFound = errors.New("agent not found")

// ErrContextTooLarge 表示系统提示词与当前请求已经超过配置的模型上下文上限。
// 历史消息可以裁剪，但当前请求不会被静默截断。
var ErrContextTooLarge = errors.New("agent context exceeds configured token limit")

// ErrTurnConflict 表示同一会话中的 turn_id 已绑定到不同的请求内容。
// 同内容网络重试可安全重放，不同内容必须生成新的 turn_id。
var ErrTurnConflict = errors.New("agent turn id is already bound to a different request")

// ErrInvalidInput 表示推荐请求未满足 Agent 业务入口的参数边界。
var ErrInvalidInput = errors.New("invalid agent recommendation input")

// Structured demand must never fall through to legacy selection (including
// direct Agent/selector calls). Only the separately enabled executor consumes it.
var ErrDemandNotExecutable = status.Error(codes.FailedPrecondition, "structured demand selection is not available")

// 文本约束错误保留 InvalidInput 分类，禁止触发兜底；公共文案由 RPC 层映射。
// 不携带用户原文，避免把查询或内部解析细节暴露到日志和响应。
var (
	ErrBudgetCurrency = fmt.Errorf("%w: budget currency must be CNY", ErrInvalidInput)
	ErrBudgetText     = fmt.Errorf("%w: invalid or ambiguous budget text", ErrInvalidInput)
	ErrItemLimitText  = fmt.Errorf("%w: invalid or ambiguous item limit text", ErrInvalidInput)
)

// ErrUnsafeResult 表示 Agent 结果违反业务约束或与商品事实不符，禁止保存和返回。
var ErrUnsafeResult = errors.New("unsafe agent recommendation result")

// ErrStreamInterrupted prevents retries/fallback after streamed execution may
// have exposed provisional output or performed tools. Keep the underlying
// status/context error for public mapping and cancellation checks.
var ErrStreamInterrupted = errors.New("agent stream interrupted")
var ErrStreamLimit = errors.New("agent stream limit exceeded")

// IsExecutionStopped 识别不得转为工具自我恢复或规则降级的终止/权限错误。
func IsExecutionStopped(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrStreamInterrupted) {
		return true
	}
	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Unauthenticated, codes.PermissionDenied:
		return true
	default:
		return false
	}
}
