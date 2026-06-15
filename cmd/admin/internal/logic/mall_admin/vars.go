package mall_admin

import (
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"
)

func productToType(p *pb.Product) types.AdminProductItem {
	if p == nil {
		return types.AdminProductItem{}
	}
	return types.AdminProductItem{
		Id:         p.Id,
		SpuCode:    p.SpuCode,
		Name:       p.Name,
		CategoryId: p.CategoryId,
		Brand:      p.Brand,
		Status:     p.Status,
		MainImage:  p.MainImage,
		Detail:     p.Detail,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func skuToType(s *pb.Sku) types.AdminSkuItem {
	if s == nil {
		return types.AdminSkuItem{}
	}
	return types.AdminSkuItem{
		Id:        s.Id,
		ProductId: s.ProductId,
		SkuCode:   s.SkuCode,
		Name:      s.Name,
		Specs:     s.Specs,
		Price:     s.Price,
		Stock:     s.Stock,
		Sold:      s.Sold,
		Status:    s.Status,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

func orderToType(o *pb.Order) types.AdminOrderResp {
	items := make([]types.AdminOrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, types.AdminOrderItem{
			ProductId:   it.ProductId,
			SkuId:       it.SkuId,
			SkuName:     it.SkuName,
			Price:       it.Price,
			Quantity:    it.Quantity,
			TotalAmount: it.TotalAmount,
			Snapshot:    it.Snapshot,
		})
	}
	return types.AdminOrderResp{
		Id:             o.Id,
		UserId:         o.UserId,
		TotalAmount:    o.TotalAmount,
		Status:         o.Status,
		PayType:        o.PayType,
		PayTime:        o.PayTime,
		Remark:         o.Remark,
		Snapshot:       o.Snapshot,
		IdempotencyKey: o.IdempotencyKey,
		Items:          items,
		CreatedAt:      o.CreatedAt,
		UpdatedAt:      o.UpdatedAt,
	}
}
