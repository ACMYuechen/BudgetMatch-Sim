// Code scaffolded by goctl. No recover, Safe to edit.

package agent

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"

	"github.com/zeromicro/go-zero/core/logx"
)

type AgentRecommendStreamLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type StreamEvent struct {
	Event string
	Data  any
}

func NewAgentRecommendStreamLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentRecommendStreamLogic {
	return &AgentRecommendStreamLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AgentRecommendStreamLogic) AgentRecommendStream(req *types.AgentRecommendReq, emit func(StreamEvent) error) error {
	if err := emit(StreamEvent{Event: "request.accepted", Data: map[string]any{
		"query":           req.Query,
		"budget_cents":    req.BudgetCents,
		"max_items":       req.MaxItems,
		"conversation_id": req.ConversationId,
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
	})
	if err != nil {
		_ = emit(StreamEvent{Event: "error", Data: map[string]any{
			"message": err.Error(),
		}})
		return err
	}

	resp := mapRecommendResp(rpcResp)
	if err := emit(StreamEvent{Event: "recommendation.final", Data: resp}); err != nil {
		return err
	}

	return emit(StreamEvent{Event: "done", Data: map[string]any{
		"ok": true,
	}})
}
