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
		Id:           p.Id,
		UserId:       p.UserId,
		Name:         p.Name,
		Content:      p.Content,
		Image:        p.Image,
		Providor:     p.Providor,
		Status:       p.Status,
		AgentComment: p.AgentComment,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

func skuToType(s *pb.Sku) types.AdminSkuItem {
	if s == nil {
		return types.AdminSkuItem{}
	}
	return types.AdminSkuItem{
		Id:           s.Id,
		ProductId:    s.ProductId,
		Name:         s.Name,
		Specs:        s.Specs,
		Price:        s.Price,
		Stock:        s.Stock,
		Sold:         s.Sold,
		Status:       s.Status,
		AgentComment: s.AgentComment,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

func orderToType(o *pb.Order) types.AdminOrderResp {
	items := make([]types.AdminOrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, types.AdminOrderItem{
			ProductId:      it.ProductId,
			SkuId:          it.SkuId,
			SkuName:        it.SkuName,
			Price:          it.Price,
			Quantity:       it.Quantity,
			DiscountAmount: it.DiscountAmount,
			TotalAmount:    it.TotalAmount,
			Snapshot:       it.Snapshot,
		})
	}
	return types.AdminOrderResp{
		Id:             o.Id,
		UserId:         o.UserId,
		OriginalAmount: o.OriginalAmount,
		DiscountAmount: o.DiscountAmount,
		PayAmount:      o.PayAmount,
		Status:         int32(o.Status),
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

func outboxEventToType(event *pb.OrderOutboxEvent) types.AdminOrderOutboxEvent {
	if event == nil {
		return types.AdminOrderOutboxEvent{}
	}
	return types.AdminOrderOutboxEvent{
		Id:          event.Id,
		AggregateId: event.AggregateId,
		EventType:   event.EventType,
		DedupKey:    event.DedupKey,
		Topic:       event.Topic,
		Tag:         event.Tag,
		MessageKey:  event.MessageKey,
		Payload:     event.Payload,
		Status:      event.Status,
		Attempts:    event.Attempts,
		MaxAttempts: event.MaxAttempts,
		NextRetryAt: event.NextRetryAt,
		LockedUntil: event.LockedUntil,
		LastError:   event.LastError,
		PublishedAt: event.PublishedAt,
		CreatedAt:   event.CreatedAt,
		UpdatedAt:   event.UpdatedAt,
	}
}
