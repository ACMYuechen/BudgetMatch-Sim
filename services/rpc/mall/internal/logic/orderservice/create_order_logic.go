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

	// 1. 幂等性检查
	idempKey := idempotencyKey(in.IdempotencyKey)
	cached, err := l.svcCtx.Redis.Get(l.ctx, idempKey).Result()
	if err == nil && cached != "" {
		// 返回已存在的订单 ID
		return &pb.CreateOrderResp{OrderId: cached, Status: int32(mall_orders.OrderStatusPending)}, nil
	} else if err != redis.Nil && err != nil {
		l.Logger.Errorf("failed to get idempotency key: %v", err)
	}

	// 2. 校验 SKU
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil {
		return nil, errors.MallSkuNotFound
	}
	if sku.Status != 1 {
		return nil, errors.MallSkuNotFound
	}
	if int64(sku.Stock) < in.Quantity {
		return nil, errors.MallStockNotEnough
	}

	// 3. 校验商品
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, sku.ProductId)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil || product.Status != 1 {
		return nil, errors.MallProductNotFound
	}

	// 4. 在数据库事务中创建订单
	orderID := uuid.New().String()
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
		Id:             orderID,
		UserId:         in.UserId,
		OriginalAmount: originalAmount,
		DiscountAmount: discountAmount,
		PayAmount:      payAmount,
		Status:         mall_orders.OrderStatusPending,
		Remark:         in.Remark,
		Snapshot:       string(snapshotJSON),
		IdempotencyKey: in.IdempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	item := &mall_order_items.MallOrderItems{
		Id:             uuid.New().String(),
		OrderId:        orderID,
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
		SkuID    string
		Quantity int64
	}{{
		SkuID:    sku.Id,
		Quantity: in.Quantity,
	}}

	// 5. 在事务中创建订单、订单项并扣减库存
	err = l.svcCtx.DB.Transaction(func(tx *gorm.DB) error {
		// 写入订单主表
		if err := l.svcCtx.OrderStore.InsertTx(tx, order); err != nil {
			return err
		}

		// 写入订单项
		if err := l.svcCtx.OrderItemStore.InsertBatchTx(tx, []*mall_order_items.MallOrderItems{item}); err != nil {
			return err
		}

		// 扣减 SKU 库存（乐观锁）
		now := time.Now()
		for _, d := range deductions {
			ok, err := l.svcCtx.SkuStore.DeductStockTx(tx, d.SkuID, d.Quantity, now)
			if err != nil {
				return err
			}
			if !ok {
				return errors.MallStockNotEnough
			}
		}

		return nil
	})
	if err != nil {
		if err == errors.MallStockNotEnough {
			return nil, errors.MallStockNotEnough
		}
		l.Logger.Errorf("failed to create order: %v", err)
		return nil, errors.Database
	}

	// 6. 记录幂等键
	_ = l.svcCtx.Redis.Set(l.ctx, idempKey, orderID, 24*time.Hour).Err()

	// 7. 异步发送 RocketMQ 事件
	if l.svcCtx.OrderEventProducer != nil {
		event := mq.OrderEvent{
			OrderID:        orderID,
			UserID:         in.UserId,
			SkuID:          sku.Id,
			Quantity:       in.Quantity,
			Status:         int32(mall_orders.OrderStatusPending),
			IdempotencyKey: in.IdempotencyKey,
		}
		l.svcCtx.OrderEventProducer.PublishCreatedAsync(event)
	}

	return &pb.CreateOrderResp{OrderId: orderID, Status: int32(mall_orders.OrderStatusPending)}, nil
}
