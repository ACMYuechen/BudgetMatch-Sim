package orderservicelogic

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/mq"
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
	if in.Quantity <= 0 {
		in.Quantity = 1
	}

	// 1. idempotency check
	idempKey := idempotencyKey(in.IdempotencyKey)
	cached, err := l.svcCtx.Redis.Get(l.ctx, idempKey).Result()
	if err == nil && cached != "" {
		// return existing order id
		return &pb.CreateOrderResp{OrderId: cached, Status: int32(OrderStatusPending)}, nil
	} else if err != redis.Nil && err != nil {
		l.Logger.Errorf("failed to get idempotency key: %v", err)
	}

	// 2. validate SKU
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.ErrDatabase
	}
	if sku == nil {
		return nil, errors.ErrMallSkuNotFound
	}
	if sku.Status != 1 {
		return nil, errors.ErrMallSkuNotFound
	}
	if sku.Stock < in.Quantity {
		return nil, errors.ErrMallStockNotEnough
	}

	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, sku.ProductId)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.ErrDatabase
	}
	if product == nil || product.Status != 1 {
		return nil, errors.ErrMallProductNotFound
	}

	// 3. create order in DB transaction
	orderID := uuid.New().String()
	totalAmount := sku.Price * in.Quantity
	now := time.Now()

	snapshot := map[string]any{
		"product_id":   product.Id,
		"product_name": product.Name,
		"sku_id":       sku.Id,
		"sku_name":     sku.Name,
		"sku_code":     sku.SkuCode,
		"price":        sku.Price,
		"quantity":     in.Quantity,
	}
	snapshotJSON, _ := json.Marshal(snapshot)

	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// insert order
		order := &mall_orders.MallOrders{
			Id:             orderID,
			UserId:         in.UserId,
			TotalAmount:    totalAmount,
			Status:         OrderStatusPending,
			Remark:         in.Remark,
			Snapshot:       string(snapshotJSON),
			IdempotencyKey: in.IdempotencyKey,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		// insert order item
		item := &mall_order_items.MallOrderItems{
			OrderId:     orderID,
			ProductId:   product.Id,
			SkuId:       sku.Id,
			SkuName:     sku.Name,
			Price:       sku.Price,
			Quantity:    in.Quantity,
			TotalAmount: totalAmount,
			Snapshot:    string(snapshotJSON),
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}

		// deduct stock with condition
		result := tx.Exec(
			"UPDATE product_skus SET stock = stock - ?, sold = sold + ?, updated_at = ? WHERE id = ? AND stock >= ?",
			in.Quantity, in.Quantity, now, sku.Id, in.Quantity,
		)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.ErrMallStockNotEnough
		}

		return nil
	})

	if err != nil {
		if err == errors.ErrMallStockNotEnough {
			return nil, errors.ErrMallStockNotEnough
		}
		l.Logger.Errorf("failed to create order: %v", err)
		return nil, errors.ErrDatabase
	}

	// 4. record idempotency key
	_ = l.svcCtx.Redis.Set(l.ctx, idempKey, orderID, 24*time.Hour).Err()

	// 5. send RocketMQ event async
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID:        orderID,
			UserID:         in.UserId,
			SkuID:          sku.Id,
			Quantity:       in.Quantity,
			Status:         int32(OrderStatusPending),
			IdempotencyKey: in.IdempotencyKey,
		}
		if err := l.svcCtx.OrderEventProducer.PublishCreated(l.ctx, event); err != nil {
			l.Logger.Errorf("failed to send order created event: %v", err)
			// do not fail order creation
		}
	}

	return &pb.CreateOrderResp{OrderId: orderID, Status: int32(OrderStatusPending)}, nil
}
