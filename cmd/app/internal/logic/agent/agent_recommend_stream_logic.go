// Code scaffolded by goctl. No recover, Safe to edit.

package agent

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// AgentRecommendStreamLogic serves negotiated v1 RPC events or legacy unary SSE.
type AgentRecommendStreamLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// StreamEvent 是逻辑层向 SSE handler 输出的事件名称与负载。
type StreamEvent struct {
	ID    string
	Event string
	Data  any
}

// NewAgentRecommendStreamLogic 创建流式 Agent 推荐逻辑。
func NewAgentRecommendStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentRecommendStreamLogic {
	return &AgentRecommendStreamLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// legacyRecommendStream is selected before execution, never as a failed v1 retry.
func (l *AgentRecommendStreamLogic) legacyRecommendStream(req *types.AgentRecommendReq, emit func(StreamEvent) error) error {
	// 流式请求与普通推荐使用相同的可信用户身份来源。
	_, err := request.MustUserId(l.ctx)
	if err != nil {
		return err
	}
	if err := emit(StreamEvent{Event: "request.accepted", Data: map[string]any{
		"conversation_id": req.ConversationId,
		"turn_id":         req.TurnId,
	}}); err != nil {
		return err
	}

	if err := emit(StreamEvent{Event: "rpc.started", Data: map[string]any{
		"service": "agent.rpc",
		"method":  "Recommend",
	}}); err != nil {
		return err
	}

	rpcResp, err := l.svcCtx.AgentClient.Recommend(l.ctx, &recommendservice.RecommendReq{
		Query:          req.Query,
		BudgetCents:    req.BudgetCents,
		MaxItems:       int32(req.MaxItems),
		ConversationId: req.ConversationId,
		TurnId:         req.TurnId,
	})
	if err != nil {
		_ = emit(StreamEvent{Event: "error", Data: publicStreamError(err)})
		return err
	}

	resp := mapRecommendResp(rpcResp)
	if err := emit(StreamEvent{Event: "recommendation.final", Data: resp}); err != nil {
		return err
	}

	if err := emit(StreamEvent{Event: "done", Data: map[string]any{
		"ok": true,
	}}); err != nil {
		return err
	}

	return nil
}

// publicStreamError 复用普通 HTTP 接口的错误转换，避免把 gRPC 内部错误文本暴露给前端。
func publicStreamError(err error) any {
	if _, known := apperrors.AsAppError(err); !known {
		if _, grpcError := status.FromError(err); !grpcError {
			err = apperrors.Internal
		}
	}
	_, response := apperrors.HTTPErrorHandler(err)
	return response
}
