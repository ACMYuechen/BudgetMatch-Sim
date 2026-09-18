package recommendservicelogic

import (
	"context"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type PlanDemandLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPlanDemandLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PlanDemandLogic {
	return &PlanDemandLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *PlanDemandLogic) PlanDemand(in *pb.PlanDemandReq) (*pb.PlanDemandResp, error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.RecommendService.PlanDemand(l.ctx, agentcore.Input{
		Query: in.GetQuery(), BudgetCents: in.GetBudgetCents(), MaxItems: in.GetMaxItems(),
		UserId: userId, ConversationId: in.GetConversationId(), TurnId: in.GetTurnId(),
	}, in.GetDemandPatch())
	if err != nil {
		err = mapRecommendError(err)
		l.Logger.Errorf("return error_code: %s", safety.ErrorCode(err))
		return nil, err
	}
	return &pb.PlanDemandResp{Intent: toPBIntent(result.Intent), Status: result.Status,
		Conflicts: toPBConflicts(result.DemandConflicts), ConversationId: result.ConversationId,
		ConversationTitle: result.ConversationTitle, TurnId: result.TurnId}, nil
}
