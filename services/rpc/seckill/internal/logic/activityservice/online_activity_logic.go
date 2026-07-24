package activityservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type OnlineActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewOnlineActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OnlineActivityLogic {
	return &OnlineActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *OnlineActivityLogic) OnlineActivity(in *pb.OnlineActivityReq) (*pb.OnlineActivityResp, error) {
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityNotFound)
		return nil, errors.SeckillActivityNotFound
	}

	activity.Status = 1
	if err := l.svcCtx.ActivityStore.Update(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to update activity status: %v", err)
		return nil, errors.Database
	}

	return &pb.OnlineActivityResp{Success: true}, nil
}
