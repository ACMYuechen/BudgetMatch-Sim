// Package safety 只输出允许公开的执行元数据，不以截断代替脱敏。
package safety

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrOutputLimit = errors.New("tool output exceeds limit")

// ErrorCode 不返回底层错误文本；类别不受错误消息内容影响。
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	case errors.Is(err, ErrOutputLimit):
		return "output_limit"
	case errors.Is(err, agent.ErrInvalidInput):
		return "invalid_argument"
	case errors.Is(err, agent.ErrContextTooLarge):
		return "context_limit"
	case errors.Is(err, agent.ErrUnsafeResult):
		return "unsafe_result"
	case errors.Is(err, agent.ErrTurnConflict):
		return "turn_conflict"
	}
	var syntax *json.SyntaxError
	var valueType *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &valueType) {
		return "invalid_argument"
	}
	if s, ok := status.FromError(err); ok && s.Code() != codes.Unknown {
		return "rpc_" + strings.ToLower(s.Code().String())
	}
	return "execution_failed"
}

type protectedError struct{ cause error }

func (e protectedError) Error() string { return "agent execution failed: " + ErrorCode(e.cause) }
func (e protectedError) Unwrap() error { return e.cause }

// 显式重建状态，避免 gRPC 通过 Unwrap 透传上游 DebugInfo/Details 中的敏感正文。
func (e protectedError) GRPCStatus() *status.Status {
	code := status.Code(e.cause)
	if errors.Is(e.cause, context.Canceled) {
		code = codes.Canceled
	} else if errors.Is(e.cause, context.DeadlineExceeded) {
		code = codes.DeadlineExceeded
	}
	return status.New(code, e.Error())
}

// Protect 在日志/RPC 边界隐藏原始文本，同时保留 errors.Is 与 gRPC 状态的判定能力。
func Protect(err error) error {
	if err == nil {
		return nil
	}
	return protectedError{cause: err}
}

// Label 只明文记录内置静态标识；外部模型/工具自报名称使用稳定摘要，避免名称夹带密钥。
func Label(value string) string {
	switch value {
	case "", "search_products", "select_bundle", "read_file", "write_file", "model",
		"ChatModel", "Tool", "Retriever", "Embedding", "Indexer", "Loader", "Graph", "Lambda",
		"OpenAI", "rag.sync", "llm", "recommend_agent", "mall.product_provider", "mock.product_provider", "rag.pgvector", "rag.hybrid_rrf":
		return value
	default:
		return fmt.Sprintf("external_%x", sha256.Sum256([]byte(value)))[:25]
	}
}

var externalLabel = regexp.MustCompile(`^external_[0-9a-f]{16}$`)
var numericDetail = regexp.MustCompile(`^(status=ok output_bytes=[0-9]{1,10} duration_ms=[0-9]{1,12}|loaded [0-9]{1,10} candidates)$`)
var errorDetail = regexp.MustCompile(`^error_code=(canceled|deadline_exceeded|permission_denied|not_found|already_exists|output_limit|invalid_argument|context_limit|unsafe_result|turn_conflict|execution_failed|rpc_(canceled|deadlineexceeded|permissiondenied|unauthenticated|unavailable|internal|unknown|invalidargument|notfound|alreadyexists|resourceexhausted|failedprecondition|aborted|outofrange|unimplemented|dataloss)) duration_ms=[0-9]{1,12}$`)

// ToolCalls 是持久化和公开响应的最后一道边界，也过滤旧记录；不修改调用方切片。
// 只允许固定说明与已知格式的数值元数据，JSON 正文、路径和原始错误一律丢弃。
func ToolCalls(calls []agent.ToolCall) []agent.ToolCall {
	if calls == nil {
		return nil
	}
	out := make([]agent.ToolCall, len(calls))
	for i, call := range calls {
		call.Name = toolLabel(call.Name)
		switch call.Detail {
		case "", "status=ok", "status=failed", "eino react orchestration",
			"model produced no bundle; used deterministic selection",
			"tool limits capped by user constraints",
			"primary execution failed; used validated fallback",
			"primary result rejected by constraints; used validated fallback":
		default:
			if len(call.Detail) > 128 || (!numericDetail.MatchString(call.Detail) && !errorDetail.MatchString(call.Detail)) {
				call.Detail = "status=failed"
				if call.Success {
					call.Detail = "status=ok"
				}
			}
		}
		out[i] = call
	}
	return out
}

func toolLabel(name string) string {
	switch name {
	case "selector.fallback", "constraints.adjusted", "candidate.verify", "retrieval.keyword", "retrieval.vector", "retrieval.expand", "retrieval.fusion", "retrieval.conflict", "demand.demo_search", "demand.demo_recheck":
		return name
	}
	for _, prefix := range []string{"tool.", "llm.", "primary."} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			if externalLabel.MatchString(rest) {
				return name
			}
			return prefix + Label(rest)
		}
	}
	if externalLabel.MatchString(name) {
		return name
	}
	return Label(name)
}
