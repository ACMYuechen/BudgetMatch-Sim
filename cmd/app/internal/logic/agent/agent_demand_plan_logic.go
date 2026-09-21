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

type AgentDemandPlanLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Agent 需求规划（不执行推荐）
func NewAgentDemandPlanLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentDemandPlanLogic {
	return &AgentDemandPlanLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AgentDemandPlanLogic) AgentDemandPlan(req *types.AgentDemandPlanReq) (resp *types.AgentDemandPlanResp, err error) {
	if _, err := request.MustUserId(l.ctx); err != nil {
		return nil, err
	}
	rpcResp, err := l.svcCtx.AgentClient.PlanDemand(l.ctx, &recommendservice.PlanDemandReq{
		Query: req.Query, BudgetCents: req.BudgetCents, MaxItems: int32(req.MaxItems),
		ConversationId: req.ConversationId, TurnId: req.TurnId, DemandPatch: req.DemandPatch,
	})
	if err != nil {
		return nil, err
	}
	return &types.AgentDemandPlanResp{Intent: mapIntent(rpcResp.GetIntent()), Status: rpcResp.GetStatus(),
		Conflicts: mapDemandConflicts(rpcResp.GetConflicts()), ConversationId: rpcResp.GetConversationId(),
		ConversationTitle: rpcResp.GetConversationTitle(), TurnId: rpcResp.GetTurnId()}, nil
}
