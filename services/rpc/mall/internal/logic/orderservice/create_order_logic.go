package orderservicelogic

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
	"budgetmatch-sim/services/rpc/mall/internal/outbox"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_items"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type CreateOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrderLogic {
	return &CreateOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateOrderLogic) CreateOrder(in *pb.CreateOrderReq) (*pb.CreateOrderResp, error) {
	if in == nil {
		return nil, errors.Invalid
	}
	userId, err := interceptor.UserScope(l.ctx, in.UserId, false)
	if err != nil {
		return nil, err
	}
	if in.IdempotencyKey == "" || in.SkuId == "" {
		return nil, errors.Invalid
	}
	if in.Quantity <= 0 {
		in.Quantity = 1
	}

	// 1. 幂等性检查
	storedKey := scopedIdempotencyKey(userId, in.IdempotencyKey)
	idempKey := idempotencyKey(storedKey)
	cached, err := l.svcCtx.Redis.Get(l.ctx, idempKey).Result()
	if err == nil && cached != "" {
		order, findErr := l.svcCtx.OrderStore.FindOne(l.ctx, cached)
		if findErr != nil {
			return nil, errors.Database
		}
		return l.replayOrder(in, userId, order)
	} else if err != redis.Nil && err != nil {
		l.Logger.Errorf("failed to get idempotency key: %v", err)
	}

	// Database uniqueness also survives cache expiry and concurrent requests.
	existing, err := l.svcCtx.OrderStore.FindByIdempotencyKey(l.ctx, storedKey)
	if err != nil {
		return nil, errors.Database
	}
	if existing != nil {
		return l.replayOrder(in, userId, existing)
	}

	// Preserve retries of pre-upgrade orders, but never replay another owner's
	// legacy globally keyed record.
	legacy, err := l.svcCtx.OrderStore.FindByIdempotencyKey(l.ctx, in.IdempotencyKey)
	if err != nil {
		return nil, errors.Database
	}
	if legacy != nil && legacy.UserId == userId {
		return l.replayOrder(in, userId, legacy)
	}

	// 2. 校验 SKU
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil {
		l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
		return nil, errors.MallSkuNotFound
	}
	if sku.Status != 1 {
		l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
		return nil, errors.MallSkuNotFound
	}
	if int64(sku.Stock) < in.Quantity {
		l.Logger.Errorf("return error: %v", errors.MallStockNotEnough)
		return nil, errors.MallStockNotEnough
	}

	// 3. 校验商品
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, sku.ProductId)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil || product.Status != 1 {
		l.Logger.Errorf("return error: %v", errors.MallProductNotFound)
		return nil, errors.MallProductNotFound
	}

	// 4. 在数据库事务中创建订单
	orderId := mall_orders.NewMallOrderId()
	originalAmount := sku.Price * in.Quantity
	discountAmount := int64(0)
	payAmount := originalAmount - discountAmount
	now := time.Now()

	snapshot := map[string]any{
		"product_id":   product.Id,
		"product_name": product.Name,
		"sku_id":       sku.Id,
		"sku_name":     sku.Name,
		"price":        sku.Price,
		"quantity":     in.Quantity,
	}
	snapshotJSON, _ := json.Marshal(snapshot)

	order := &mall_orders.MallOrders{
		Id:             orderId,
		UserId:         userId,
		OriginalAmount: originalAmount,
		DiscountAmount: discountAmount,
		PayAmount:      payAmount,
		Status:         mall_orders.OrderStatusPending,
		Remark:         in.Remark,
		Snapshot:       string(snapshotJSON),
		IdempotencyKey: storedKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	item := &mall_order_items.MallOrderItems{
		Id:             mall_order_items.NewMallOrderItemId(),
		OrderId:        orderId,
		ProductId:      product.Id,
		SkuId:          sku.Id,
		SkuName:        sku.Name,
		Price:          sku.Price,
		Quantity:       in.Quantity,
		DiscountAmount: discountAmount,
		TotalAmount:    payAmount,
		Snapshot:       string(snapshotJSON),
	}

	deductions := []struct {
		SkuId    string
		Quantity int64
	}{{
		SkuId:    sku.Id,
		Quantity: in.Quantity,
	}}

	// 5. 在事务中创建订单、订单项并扣减库存
	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 写入订单主表
		if err := l.svcCtx.OrderStore.InsertTx(tx, order); err != nil {
			l.Logger.Errorf("return error: %v", err)
			return err
		}

		// 写入订单项
		if err := l.svcCtx.OrderItemStore.InsertBatchTx(tx, []*mall_order_items.MallOrderItems{item}); err != nil {
			l.Logger.Errorf("return error: %v", err)
			return err
		}

		// 扣减 SKU 库存（乐观锁）
		now := time.Now()
		for _, d := range deductions {
			ok, err := l.svcCtx.SkuStore.DeductStockTx(tx, d.SkuId, d.Quantity, now)
			if err != nil {
				l.Logger.Errorf("return error: %v", err)
				return err
			}
			if !ok {
				l.Logger.Errorf("return error: %v", errors.MallStockNotEnough)
				return errors.MallStockNotEnough
			}
		}

		outboxEvent, err := outbox.NewOrderEvent(mq.EventTypeCreated, now, mq.OrderEvent{
			OrderId:  orderId,
			UserId:   userId,
			SkuId:    sku.Id,
			Quantity: in.Quantity,
			Status:   int32(mall_orders.OrderStatusPending),
		})
		if err != nil {
			l.Logger.Errorf("failed to build order created event: order_id=%s error=%v", order.Id, err)
			return err
		}
		if err := l.svcCtx.OrderOutboxStore.InsertTx(tx, outboxEvent); err != nil {
			l.Logger.Errorf("failed to insert order created outbox event: order_id=%s event_id=%s error=%v", orderId, outboxEvent.Id, err)
			return err
		}

		return nil
	})
	if err != nil {
		if err == errors.MallStockNotEnough {
			l.Logger.Errorf("return error: %v", errors.MallStockNotEnough)
			return nil, errors.MallStockNotEnough
		}
		l.Logger.Errorf("failed to create order: %v", err)
		return nil, errors.Database
	}

	// 6. 记录幂等键
	_ = l.svcCtx.Redis.Set(l.ctx, idempKey, orderId, 24*time.Hour).Err()

	return &pb.CreateOrderResp{OrderId: orderId, Status: pb.OrderStatus_ORDER_STATUS_PENDING}, nil
}

// Never return an order from a replay before checking ownership and request identity.
func (l *CreateOrderLogic) replayOrder(in *pb.CreateOrderReq, userId string, order *mall_orders.MallOrders) (*pb.CreateOrderResp, error) {
	if order == nil || order.UserId != userId {
		return nil, errors.MallOrderNotFound
	}
	if (order.IdempotencyKey != scopedIdempotencyKey(userId, in.IdempotencyKey) && order.IdempotencyKey != in.IdempotencyKey) || order.Remark != in.Remark {
		return nil, errors.Invalid
	}
	items, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, order.Id)
	if err != nil {
		return nil, errors.Database
	}
	if len(items) != 1 || items[0].SkuId != in.SkuId || items[0].Quantity != in.Quantity {
		return nil, errors.Invalid
	}
	return &pb.CreateOrderResp{OrderId: order.Id, Status: pb.OrderStatus(order.Status)}, nil
}
