package seckillservicelogic

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type AcquireTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAcquireTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AcquireTokenLogic {
	return &AcquireTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *AcquireTokenLogic) AcquireToken(in *pb.AcquireTokenReq) (*pb.AcquireTokenResp, error) {
	// validate activity
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

	// validate sku
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil {
		l.Logger.Errorf("return error: %v", errors.SeckillSkuNotFound)
		return nil, errors.SeckillSkuNotFound
	}
	if sku.Status != 1 {
		l.Logger.Errorf("return error: %v", errors.SeckillSkuNotFound)
		return nil, errors.SeckillSkuNotFound
	}
	if sku.ActivityId != in.ActivityId {
		l.Logger.Errorf("return error: %v", errors.SeckillSkuNotFound)
		return nil, errors.SeckillSkuNotFound
	}

	// rate limit: activity-level global sliding window (5s / 1000 requests per activity)
	activityKey := fmt.Sprintf("seckill:limit:activity:%s", in.ActivityId)
	if !l.svcCtx.ActivityRateLimiter.Allow(l.ctx, activityKey) {
		l.Logger.Errorf("return error: %v", errors.TooManyRequests)
		return nil, errors.TooManyRequests
	}

	// rate limit: user-level token bucket (capacity 5, refill 1 per 60s per user)
	userKey := fmt.Sprintf("seckill:limit:user:%s", in.UserId)
	if !l.svcCtx.UserRateLimiter.Allow(l.ctx, userKey) {
		l.Logger.Errorf("return error: %v", errors.TooManyRequests)
		return nil, errors.TooManyRequests
	}

	// generate token and store in Redis
	token := uuid.New().String()
	if err := l.svcCtx.StockManager.SetToken(token, in.SkuId, 60*time.Second); err != nil {
		l.Logger.Errorf("failed to set token: %v", err)
		return nil, errors.Internal
	}

	return &pb.AcquireTokenResp{Token: token}, nil
}
