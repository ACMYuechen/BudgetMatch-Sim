package recommendservicelogic

import (
	"context"

	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ExecuteDemandLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewExecuteDemandLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExecuteDemandLogic {
	return &ExecuteDemandLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ExecuteDemandLogic) ExecuteDemand(in *pb.ExecuteDemandReq) (*pb.RecommendResp, error) {
	userID, err := authenticatedUserId(l.ctx)
	if err != nil {
		return nil, err
	}
	result, err := l.svcCtx.RecommendService.ExecuteDemand(l.ctx, userID, in.GetConversationId(), in.GetPlanTurnId(), in.GetTurnId())
	if err != nil {
		err = mapRecommendError(err)
		l.Logger.Errorf("return error_code: %s", safety.ErrorCode(err))
		return nil, err
	}
	return toPB(result), nil
}
