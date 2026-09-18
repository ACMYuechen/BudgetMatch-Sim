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
)

type AgentDemandExecuteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 执行已确认需求（显式演示模式，非实时商城库存）
func NewAgentDemandExecuteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AgentDemandExecuteLogic {
	return &AgentDemandExecuteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AgentDemandExecuteLogic) AgentDemandExecute(req *types.AgentDemandExecuteReq) (resp *types.AgentRecommendResp, err error) {
	if _, err := request.MustUserId(l.ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, apperrors.Invalid
	}
	rpcResp, err := l.svcCtx.AgentClient.ExecuteDemand(l.ctx, &recommendservice.ExecuteDemandReq{
		ConversationId: req.ConversationId, PlanTurnId: req.PlanTurnId, TurnId: req.TurnId,
	})
	if err != nil {
		return nil, err
	}
	return mapRecommendResp(rpcResp), nil
}
