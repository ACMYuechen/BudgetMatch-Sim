package orderservicelogic

import (
	"time"

	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

func orderToPb(o *mall_orders.MallOrders, items []mall_order_items.MallOrderItems) *pb.Order {
	if o == nil {
		return nil
	}
	resp := &pb.Order{
		Id:             o.Id,
		UserId:         o.UserId,
		OriginalAmount: o.OriginalAmount,
		DiscountAmount: o.DiscountAmount,
		PayAmount:      o.PayAmount,
		Status:         pb.OrderStatus(o.Status),
		PayType:        o.PayType,
		Remark:         o.Remark,
		Snapshot:       o.Snapshot,
		IdempotencyKey: o.IdempotencyKey,
		CreatedAt:      o.CreatedAt.Format(timeLayout),
		UpdatedAt:      o.UpdatedAt.Format(timeLayout),
	}
	if !o.PayTime.IsZero() {
		resp.PayTime = o.PayTime.Format(timeLayout)
	}
	for _, it := range items {
		resp.Items = append(resp.Items, &pb.OrderItem{
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
	return resp
}

func outboxEventToPb(event *mall_order_outbox.MallOrderOutbox) *pb.OrderOutboxEvent {
	if event == nil {
		return nil
	}
	return &pb.OrderOutboxEvent{
		Id:          event.Id,
		AggregateId: event.AggregateId,
		EventType:   event.EventType,
		DedupKey:    event.DedupKey,
		Topic:       event.Topic,
		Tag:         event.Tag,
		MessageKey:  event.MessageKey,
		Payload:     event.Payload,
		Status:      int32(event.Status),
		Attempts:    int32(event.Attempts),
		MaxAttempts: int32(event.MaxAttempts),
		NextRetryAt: event.NextRetryAt.Format(timeLayout),
		LockedUntil: event.LockedUntil.Format(timeLayout),
		LastError:   event.LastError,
		PublishedAt: event.PublishedAt,
		CreatedAt:   event.CreatedAt.Format(timeLayout),
		UpdatedAt:   event.UpdatedAt.Format(timeLayout),
	}
}

func idempotencyKey(key string) string {
	return "mall:idempotency:" + key
}

// 暂时未使用
func rollbackWithBackoff(fn func() error, retries int) error {
	var err error
	for i := 0; i < retries; i++ {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(time.Duration(50*(1<<i)) * time.Millisecond)
	}
	return err
}
