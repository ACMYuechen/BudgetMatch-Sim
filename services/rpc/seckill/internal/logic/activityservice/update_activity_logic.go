package activityservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type UpdateActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateActivityLogic {
	return &UpdateActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateActivityLogic) UpdateActivity(in *pb.UpdateActivityReq) (*pb.UpdateActivityResp, error) {
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.ErrDatabase
	}
	if activity == nil {
		return nil, errors.ErrSeckillActivityNotFound
	}

	if in.Title != "" {
		activity.Title = in.Title
	}
	if in.Description != "" {
		activity.Description = in.Description
	}
	if in.BannerUrl != "" {
		activity.BannerUrl = in.BannerUrl
	}
	if in.StartTime != "" {
		startTime, err := time.Parse(time.RFC3339, in.StartTime)
		if err != nil {
			startTime, err = time.Parse("2006-01-02 15:04:05", in.StartTime)
			if err != nil {
				return nil, errors.ErrInternal
			}
		}
		activity.StartTime = startTime
	}
	if in.EndTime != "" {
		endTime, err := time.Parse(time.RFC3339, in.EndTime)
		if err != nil {
			endTime, err = time.Parse("2006-01-02 15:04:05", in.EndTime)
			if err != nil {
				return nil, errors.ErrInternal
			}
		}
		activity.EndTime = endTime
	}

	if activity.EndTime.Before(activity.StartTime) || activity.EndTime.Equal(activity.StartTime) {
		return nil, errors.ErrInternal
	}

	if err := l.svcCtx.ActivityStore.Update(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to update activity: %v", err)
		return nil, errors.ErrDatabase
	}

	return &pb.UpdateActivityResp{Success: true}, nil
}
