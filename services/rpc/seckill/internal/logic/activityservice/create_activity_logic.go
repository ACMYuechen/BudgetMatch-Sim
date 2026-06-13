package activityservicelogic

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	startTime, err := time.Parse(time.RFC3339, in.StartTime)
	if err != nil {
		startTime, err = time.Parse("2006-01-02 15:04:05", in.StartTime)
		if err != nil {
			return nil, errors.ErrInternal
		}
	}
	endTime, err := time.Parse(time.RFC3339, in.EndTime)
	if err != nil {
		endTime, err = time.Parse("2006-01-02 15:04:05", in.EndTime)
		if err != nil {
			return nil, errors.ErrInternal
		}
	}

	if endTime.Before(startTime) || endTime.Equal(startTime) {
		return nil, errors.ErrInternal
	}

	activity := &seckill_activity.SeckillActivities{
		Id:          uuid.New().String(),
		Title:       in.Title,
		Description: in.Description,
		BannerUrl:   in.BannerUrl,
		StartTime:   startTime,
		EndTime:     endTime,
		Status:      0, // default offline
	}

	if err := l.svcCtx.ActivityStore.InsertOne(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to create activity: %v", err)
		return nil, errors.ErrDatabase
	}

	return &pb.CreateActivityResp{Id: activity.Id}, nil
}
