package activityservicelogic

import (
	"time"

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
		StartTime:   a.StartTime.Format(time.RFC3339),
		EndTime:     a.EndTime.Format(time.RFC3339),
		Status:      int32(a.Status),
		CreatedAt:   a.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   a.UpdatedAt.Format(time.RFC3339),
	}
}
