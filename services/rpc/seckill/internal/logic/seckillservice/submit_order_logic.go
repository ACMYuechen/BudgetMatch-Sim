package seckillservicelogic

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/dlock"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_order"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type SubmitOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSubmitOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubmitOrderLogic {
	return &SubmitOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SubmitOrderLogic) SubmitOrder(in *pb.SubmitOrderReq) (*pb.SubmitOrderResp, error) {
	// 1. 原子校验并消费 token（GETDEL），保证一次性使用，避免并发复用
	skuIDFromToken, err := l.svcCtx.StockManager.ConsumeToken(in.Token)
	if err == redis.Nil {
		l.Logger.Errorf("return error: %v", errors.SeckillTokenInvalid)
		return nil, errors.SeckillTokenInvalid
	}
	if err != nil {
		l.Logger.Errorf("failed to consume token: %v", err)
		return nil, errors.Internal
	}
	if skuIDFromToken != in.SkuId {
		l.Logger.Errorf("return error: %v", errors.SeckillTokenInvalid)
		return nil, errors.SeckillTokenInvalid
	}

	// 2. 校验活动时间和状态
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.ActivityId)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityNotFound)
		return nil, errors.SeckillActivityNotFound
	}
	if activity.Status != 1 && activity.Status != 2 {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityEnded)
		return nil, errors.SeckillActivityEnded
	}
	now := time.Now()
	if now.Before(activity.StartTime) {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityNotStart)
		return nil, errors.SeckillActivityNotStart
	}
	if now.After(activity.EndTime) {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityEnded)
		return nil, errors.SeckillActivityEnded
	}

	// 3. 校验 SKU
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil || sku.Status != 1 || sku.ActivityId != in.ActivityId {
		l.Logger.Errorf("return error: %v", errors.SeckillSkuNotFound)
		return nil, errors.SeckillSkuNotFound
	}

	// 4. 活动级限流：每个活动 5 秒内最多处理 1000 个请求
	activityKey := fmt.Sprintf("seckill:limit:activity:%s", in.ActivityId)
	if !l.svcCtx.ActivityRateLimiter.Allow(l.ctx, activityKey) {
		l.Logger.Errorf("return error: %v", errors.TooManyRequests)
		return nil, errors.TooManyRequests
	}

	// 5. 用户级限流：令牌桶容量为 5，每 60 秒补充 1 个令牌
	userKey := fmt.Sprintf("seckill:limit:user:%s", in.UserId)
	if !l.svcCtx.UserRateLimiter.Allow(l.ctx, userKey) {
		l.Logger.Errorf("return error: %v", errors.TooManyRequests)
		return nil, errors.TooManyRequests
	}

	// 6. 检查用户是否已购买
	existingOrder, err := l.svcCtx.OrderStore.FindByActivityAndSkuAndUser(l.ctx, in.ActivityId, in.SkuId, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to check existing order: %v", err)
		return nil, errors.Database
	}
	if existingOrder != nil && existingOrder.Status == 1 {
		l.Logger.Errorf("return error: %v", errors.SeckillAlreadyPurchased)
		return nil, errors.SeckillAlreadyPurchased
	}

	// 7. 从 Redis 扣减库存
	// 对于低库存 SKU，使用 etcd 分布式锁兜底，防止 Redis 主从切换等极端场景下超卖
	qty := int64(in.Quantity)
	if qty <= 0 {
		qty = 1
	}

	var remain int64
	if int64(sku.Stock) <= l.svcCtx.LowStockThreshold() {
		lock, err := l.svcCtx.LockManager.NewLock(fmt.Sprintf("/seckill/lock/stock/%s/%s", in.ActivityId, in.SkuId), 10)
		if err != nil {
			l.Logger.Errorf("failed to create etcd lock: %v", err)
			return nil, errors.Internal
		}
		defer lock.Close()

		err = dlock.WithLock(l.ctx, lock, func() error {
			r, err := l.svcCtx.StockManager.Deduct(in.ActivityId, in.SkuId, qty)
			if err != nil {
				l.Logger.Errorf("return error: %v", err)
				return err
			}
			remain = r
			return nil
		})
		if err != nil {
			if err == errors.SeckillStockNotEnough {
				l.Logger.Errorf("return error: %v", errors.SeckillStockNotEnough)
				return nil, errors.SeckillStockNotEnough
			}
			l.Logger.Errorf("failed to deduct stock with lock: %v", err)
			return nil, errors.Internal
		}
	} else {
		r, err := l.svcCtx.StockManager.Deduct(in.ActivityId, in.SkuId, qty)
		if err != nil {
			if err == errors.SeckillStockNotEnough {
				l.Logger.Errorf("return error: %v", errors.SeckillStockNotEnough)
				return nil, errors.SeckillStockNotEnough
			}
			l.Logger.Errorf("failed to deduct stock: %v", err)
			return nil, errors.Internal
		}
		remain = r
	}
	if remain < 0 {
		l.Logger.Errorf("return error: %v", errors.SeckillStockNotEnough)
		return nil, errors.SeckillStockNotEnough
	}

	// 8. 生成订单 ID 并写入消息流
	orderID := seckill_order.NewSeckillOrderId()
	totalAmount := sku.SeckillPrice * qty

	_, err = l.svcCtx.Redis.XAdd(l.ctx, &redis.XAddArgs{
		Stream: "seckill:order:stream",
		Values: map[string]interface{}{
			"order_id":     orderID,
			"activity_id":  in.ActivityId,
			"sku_id":       in.SkuId,
			"user_id":      in.UserId,
			"quantity":     qty,
			"total_amount": totalAmount,
		},
	}).Result()
	if err != nil {
		l.Logger.Errorf("failed to add to stream: %v", err)
		// 回滚库存
		l.svcCtx.StockManager.Rollback(in.ActivityId, in.SkuId, qty)
		l.Logger.Errorf("return error: %v", errors.SeckillSubmitFailed)
		return nil, errors.SeckillSubmitFailed
	}

	return &pb.SubmitOrderResp{
		OrderId: orderID,
		Status:  0, // 排队中
	}, nil
}
