package activityservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type OfflineActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewOfflineActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OfflineActivityLogic {
	return &OfflineActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *OfflineActivityLogic) OfflineActivity(in *pb.OfflineActivityReq) (*pb.OfflineActivityResp, error) {
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		return nil, errors.SeckillActivityNotFound
	}

	activity.Status = 0
	if err := l.svcCtx.ActivityStore.Update(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to update activity status: %v", err)
		return nil, errors.Database
	}

	return &pb.OfflineActivityResp{Success: true}, nil
}
