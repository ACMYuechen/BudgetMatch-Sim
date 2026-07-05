package skuservicelogic

import (
	"time"

	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

func skuToPb(s *seckill_sku.SeckillSkus) *pb.Sku {
	if s == nil {
		return nil
	}
	return &pb.Sku{
		Id:             s.Id,
		ActivityId:     s.ActivityId,
		Title:          s.Title,
		Subtitle:       s.Subtitle,
		Pic:            s.Pic,
		OriginalPrice:  s.OriginalPrice,
		SeckillPrice:   s.SeckillPrice,
		Stock:          s.Stock,
		Sold:           s.Sold,
		LockStock:      s.LockStock,
		Status:         int32(s.Status),
		Sort:           s.Sort,
		CreatedAt:      s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      s.UpdatedAt.Format(time.RFC3339),
		MallSkuId:      s.MallSkuId,
	}
}
