// Code scaffolded by goctl. No recover, Safe to edit.

package agent

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"

	"github.com/zeromicro/go-zero/core/logx"
)

// AgentRecommendStreamLogic 负责以 SSE 事件包装推荐 RPC 的执行阶段和最终结果。
type AgentRecommendStreamLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// StreamEvent 是逻辑层向 SSE handler 输出的事件名称与负载。
type StreamEvent struct {
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

// AgentRecommendStream 依次发送受理、RPC 启动、最终结果和完成事件。
func (l *AgentRecommendStreamLogic) AgentRecommendStream(req *types.AgentRecommendReq, emit func(StreamEvent) error) error {
	// 流式请求与普通推荐使用相同的可信用户身份来源。
	_, err := request.MustUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return err
	}
	if err := emit(StreamEvent{Event: "request.accepted", Data: map[string]any{
		"query":           req.Query,
		"budget_cents":    req.BudgetCents,
		"max_items":       req.MaxItems,
		"conversation_id": req.ConversationId,
		"turn_id":         req.TurnId,
	}}); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return err
	}

	if err := emit(StreamEvent{Event: "rpc.started", Data: map[string]any{
		"service": "agent.rpc",
		"method":  "Recommend",
	}}); err != nil {
		l.Logger.Errorf("return error: %v", err)
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
		_ = emit(StreamEvent{Event: "error", Data: map[string]any{
			"message": err.Error(),
		}})
		l.Logger.Errorf("return error: %v", err)
		return err
	}

	resp := mapRecommendResp(rpcResp)
	if err := emit(StreamEvent{Event: "recommendation.final", Data: resp}); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return err
	}

	if err := emit(StreamEvent{Event: "done", Data: map[string]any{
		"ok": true,
	}}); err != nil {
		l.Logger.Errorf("return error: %v", err)
		return err
	}

	return nil
}
