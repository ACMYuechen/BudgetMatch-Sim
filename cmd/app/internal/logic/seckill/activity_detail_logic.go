// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 活动详情
func NewActivityDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityDetailLogic {
	return &ActivityDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityDetailLogic) ActivityDetail(req *types.ActivityDetailReq) (resp *types.ActivityDetailResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.GetActivity(l.ctx, &pb.GetActivityReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to get activity: %v", err)
		return nil, err
	}

	activity := rpcResp.Activity
	return &types.ActivityDetailResp{
		Activity: types.ActivityItem{
			Id:          activity.Id,
			Title:       activity.Title,
			Description: activity.Description,
			BannerUrl:   activity.BannerUrl,
			StartTime:   activity.StartTime,
			EndTime:     activity.EndTime,
			Status:      activity.Status,
			CreatedAt:   activity.CreatedAt,
		},
	}, nil
}
