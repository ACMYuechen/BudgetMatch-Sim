package activityservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type CreateActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateActivityLogic {
	return &CreateActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateActivityLogic) CreateActivity(in *pb.CreateActivityReq) (*pb.CreateActivityResp, error) {
	startTime := time.UnixMilli(in.StartTime)
	endTime := time.UnixMilli(in.EndTime)

	if endTime.Before(startTime) || endTime.Equal(startTime) {
		l.Logger.Errorf("return error: %v", errors.Internal)
		return nil, errors.Internal
	}

	activity := &seckill_activity.SeckillActivities{
		Id:          seckill_activity.NewSeckillActivityId(),
		Title:       in.Title,
		Description: in.Description,
		BannerUrl:   in.BannerUrl,
		StartTime:   startTime,
		EndTime:     endTime,
		Status:      0, // 默认下线
	}

	if err := l.svcCtx.ActivityStore.InsertOne(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to create activity: %v", err)
		return nil, errors.Database
	}

	return &pb.CreateActivityResp{Id: activity.Id}, nil
}
