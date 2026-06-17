package activityservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type ListActivitiesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListActivitiesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListActivitiesLogic {
	return &ListActivitiesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListActivitiesLogic) ListActivities(in *pb.ListActivitiesReq) (*pb.ListActivitiesResp, error) {
	var list []*pb.Activity
	var total int64

	page := int(in.Page)
	if page <= 0 {
		page = 1
	}
	pageSize := int(in.PageSize)
	if pageSize <= 0 {
		pageSize = 20
	}

	if in.Status > 0 {
		activities, t, err := l.svcCtx.ActivityStore.ListByStatus(l.ctx, int64(in.Status), page, pageSize)
		if err != nil {
			l.Logger.Errorf("failed to list activities by status: %v", err)
			return nil, errors.Database
		}
		total = t
		for _, a := range activities {
			list = append(list, activityToPb(&a))
		}
	} else {
		activities, t, err := l.svcCtx.ActivityStore.List(l.ctx, seckill_activity.SeckillActivitiesListReq{
			Page: page,
			Size: pageSize,
		})
		if err != nil {
			l.Logger.Errorf("failed to list activities: %v", err)
			return nil, errors.Database
		}
		total = t
		for _, a := range activities {
			list = append(list, activityToPb(&a))
		}
	}

	return &pb.ListActivitiesResp{
		List:      list,
		Total:     total,
		Page:      int32(page),
		PageSize:  int32(pageSize),
	}, nil
}
