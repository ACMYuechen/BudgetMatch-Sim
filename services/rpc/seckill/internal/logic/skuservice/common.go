package skuservicelogic

import (
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

func skuToPb(s *seckill_sku.SeckillSkus) *pb.Sku {
	if s == nil {
		return nil
	}
	return &pb.Sku{
		Id:            s.Id,
		ActivityId:    s.ActivityId,
		Title:         s.Title,
		Subtitle:      s.Subtitle,
		Pic:           s.Pic,
		OriginalPrice: s.OriginalPrice,
		SeckillPrice:  s.SeckillPrice,
		Stock:         int32(s.Stock),
		Sold:          int32(s.Sold),
		LockStock:     int32(s.LockStock),
		Status:        int32(s.Status),
		Sort:          int32(s.Sort),
		CreatedAt:     s.CreatedAt.UnixMilli(),
		UpdatedAt:     s.UpdatedAt.UnixMilli(),
		MallSkuId:     s.MallSkuId,
	}
}
