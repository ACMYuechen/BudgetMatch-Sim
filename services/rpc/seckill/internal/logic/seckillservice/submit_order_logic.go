package seckillservicelogic

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
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
	// 1. validate token
	skuIDFromToken, err := l.svcCtx.StockManager.GetToken(in.Token)
	if err == redis.Nil {
		return nil, errors.ErrSeckillTokenInvalid
	}
	if err != nil {
		l.Logger.Errorf("failed to get token: %v", err)
		return nil, errors.ErrInternal
	}
	if skuIDFromToken != in.SkuId {
		return nil, errors.ErrSeckillTokenInvalid
	}
	// delete token after use (one-time)
	if err := l.svcCtx.StockManager.DelToken(in.Token); err != nil {
		l.Logger.Info("failed to delete token: " + err.Error())
	}

	// 2. validate activity time and status
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.ActivityId)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.ErrDatabase
	}
	if activity == nil {
		return nil, errors.ErrSeckillActivityNotFound
	}
	if activity.Status != 1 && activity.Status != 2 {
		return nil, errors.ErrSeckillActivityNotStart
	}
	now := time.Now()
	if now.Before(activity.StartTime) {
		return nil, errors.ErrSeckillActivityNotStart
	}
	if now.After(activity.EndTime) {
		return nil, errors.ErrSeckillActivityEnded
	}

	// 3. validate sku
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.ErrDatabase
	}
	if sku == nil || sku.Status != 1 || sku.ActivityId != in.ActivityId {
		return nil, errors.ErrSeckillSkuNotFound
	}

	// 4. rate limit: activity-level global sliding window (5s / 1000 requests per activity)
	activityKey := fmt.Sprintf("seckill:limit:activity:%s", in.ActivityId)
	if !l.svcCtx.ActivityRateLimiter.Allow(l.ctx, activityKey) {
		return nil, errors.ErrTooManyRequests
	}

	// 5. rate limit: user-level token bucket (capacity 5, refill 1 per 60s per user)
	userKey := fmt.Sprintf("seckill:limit:user:%s", in.UserId)
	if !l.svcCtx.UserRateLimiter.Allow(l.ctx, userKey) {
		return nil, errors.ErrTooManyRequests
	}

	// 6. check already purchased
	existingOrder, err := l.svcCtx.OrderStore.FindByActivityAndSkuAndUser(l.ctx, in.ActivityId, in.SkuId, in.UserId)
	if err != nil {
		l.Logger.Errorf("failed to check existing order: %v", err)
		return nil, errors.ErrDatabase
	}
	if existingOrder != nil && existingOrder.Status == 1 {
		return nil, errors.ErrSeckillAlreadyPurchased
	}

	// 7. deduct stock from Redis
	qty := in.Quantity
	if qty <= 0 {
		qty = 1
	}
	remain, err := l.svcCtx.StockManager.Deduct(in.ActivityId, in.SkuId, qty)
	if err != nil {
		if err == errors.ErrSeckillStockNotEnough {
			return nil, errors.ErrSeckillStockNotEnough
		}
		l.Logger.Errorf("failed to deduct stock: %v", err)
		return nil, errors.ErrInternal
	}
	if remain < 0 {
		return nil, errors.ErrSeckillStockNotEnough
	}

	// 8. generate order_id and push to stream
	orderID := uuid.New().String()
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
		// rollback stock
		l.svcCtx.StockManager.Rollback(in.ActivityId, in.SkuId, qty)
		return nil, errors.ErrSeckillSubmitFailed
	}

	return &pb.SubmitOrderResp{
		OrderId: orderID,
		Status:  0, // queued
	}, nil
}
