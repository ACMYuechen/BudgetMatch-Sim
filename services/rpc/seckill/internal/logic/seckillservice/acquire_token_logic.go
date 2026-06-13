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

	// validate sku
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.SkuId)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.ErrDatabase
	}
	if sku == nil {
		return nil, errors.ErrSeckillSkuNotFound
	}
	if sku.Status != 1 {
		return nil, errors.ErrSeckillSkuNotFound
	}
	if sku.ActivityId != in.ActivityId {
		return nil, errors.ErrSeckillSkuNotFound
	}

	// rate limit: activity-level global sliding window (5s / 1000 requests per activity)
	activityKey := fmt.Sprintf("seckill:limit:activity:%s", in.ActivityId)
	if !l.svcCtx.ActivityRateLimiter.Allow(l.ctx, activityKey) {
		return nil, errors.ErrTooManyRequests
	}

	// rate limit: user-level token bucket (capacity 5, refill 1 per 60s per user)
	userKey := fmt.Sprintf("seckill:limit:user:%s", in.UserId)
	if !l.svcCtx.UserRateLimiter.Allow(l.ctx, userKey) {
		return nil, errors.ErrTooManyRequests
	}

	// generate token and store in Redis
	token := uuid.New().String()
	if err := l.svcCtx.StockManager.SetToken(token, in.SkuId, 60*time.Second); err != nil {
		l.Logger.Errorf("failed to set token: %v", err)
		return nil, errors.ErrInternal
	}

	return &pb.AcquireTokenResp{Token: token}, nil
}
