package activityservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type DeleteActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteActivityLogic {
	return &DeleteActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteActivityLogic) DeleteActivity(in *pb.DeleteActivityReq) (*pb.DeleteActivityResp, error) {
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityNotFound)
		return nil, errors.SeckillActivityNotFound
	}

	if err := l.svcCtx.ActivityStore.Delete(l.ctx, in.Id); err != nil {
		l.Logger.Errorf("failed to delete activity: %v", err)
		return nil, errors.Database
	}

	return &pb.DeleteActivityResp{Success: true}, nil
}
