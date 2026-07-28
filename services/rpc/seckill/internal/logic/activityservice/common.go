package activityservicelogic

import (
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

func activityToPb(a *seckill_activity.SeckillActivities) *pb.Activity {
	if a == nil {
		return nil
	}
	return &pb.Activity{
		Id:          a.Id,
		Title:       a.Title,
		Description: a.Description,
		BannerUrl:   a.BannerUrl,
		StartTime:   a.StartTime.UnixMilli(),
		EndTime:     a.EndTime.UnixMilli(),
		Status:      int32(a.Status),
		CreatedAt:   a.CreatedAt.UnixMilli(),
		UpdatedAt:   a.UpdatedAt.UnixMilli(),
	}
}
