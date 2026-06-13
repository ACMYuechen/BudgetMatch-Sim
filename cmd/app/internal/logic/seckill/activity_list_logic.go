// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ActivityListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 活动列表
func NewActivityListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ActivityListLogic {
	return &ActivityListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ActivityListLogic) ActivityList(req *types.ActivityListReq) (resp *types.ActivityListResp, err error) {
	rpcResp, err := l.svcCtx.ActivityClient.ListActivities(l.ctx, &pb.ListActivitiesReq{
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list activities: %v", err)
		return nil, errors.ErrDatabase
	}

	items := make([]types.ActivityItem, 0, len(rpcResp.List))
	for _, a := range rpcResp.List {
		items = append(items, types.ActivityItem{
			Id:          a.Id,
			Title:       a.Title,
			Description: a.Description,
			BannerUrl:   a.BannerUrl,
			StartTime:   a.StartTime,
			EndTime:     a.EndTime,
			Status:      a.Status,
			CreatedAt:   a.CreatedAt,
		})
	}

	return &types.ActivityListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
