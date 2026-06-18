package orderservicelogic

import (
	"time"

	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
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
		TotalAmount:    o.TotalAmount,
		Status:         int32(o.Status),
		PayType:        o.PayType,
		Remark:         o.Remark,
		Snapshot:       o.Snapshot,
		IdempotencyKey: o.IdempotencyKey,
		CreatedAt:      o.CreatedAt.Format(timeLayout),
		UpdatedAt:      o.UpdatedAt.Format(timeLayout),
	}
	if o.PayTime != nil {
		resp.PayTime = o.PayTime.Format(timeLayout)
	}
	for _, it := range items {
		resp.Items = append(resp.Items, &pb.OrderItem{
			ProductId:   it.ProductId,
			SkuId:       it.SkuId,
			SkuName:     it.SkuName,
			Price:       it.Price,
			Quantity:    it.Quantity,
			TotalAmount: it.TotalAmount,
			Snapshot:    it.Snapshot,
		})
	}
	return resp
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
