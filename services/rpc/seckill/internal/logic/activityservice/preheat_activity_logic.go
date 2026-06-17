package activityservicelogic

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type PreheatActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPreheatActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreheatActivityLogic {
	return &PreheatActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *PreheatActivityLogic) PreheatActivity(in *pb.PreheatActivityReq) (*pb.PreheatActivityResp, error) {
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		return nil, errors.SeckillActivityNotFound
	}

	// load all skus for this activity and preheat stock to Redis
	skus, _, err := l.svcCtx.SkuStore.ListByActivity(l.ctx, in.Id, 1, 1000)
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, errors.Database
	}

	now := time.Now()
	ttlSeconds := 86400 // 默认一天缓冲
	if activity.EndTime.Before(now) {
		// 活动已结束：保留缓冲时间用于延迟处理
	} else {
		// 活动尚未结束（包括未开始和进行中）
		ttlSeconds = int(activity.EndTime.Sub(now).Seconds()) + 86400
	}
	// 设置最大 TTL 上限（如 7 天），避免极端情况
	if ttlSeconds > 86400*7 {
		ttlSeconds = 86400 * 7
	}

	for _, sku := range skus {
		// 跳过未启用的商品
		if sku.Status != 1 {
			continue
		}
		// 计算剩余库存
		remain := sku.Stock - sku.Sold
		if err := l.svcCtx.StockManager.Preheat(in.Id, sku.Id, remain, ttlSeconds); err != nil {
			l.Logger.Info("preheat stock failed for sku " + sku.Id + ": " + err.Error())
		}
	}

	// update activity status to 2 (preheating)
	activity.Status = 2
	if err := l.svcCtx.ActivityStore.Update(l.ctx, activity); err != nil {
		l.Logger.Errorf("failed to update activity status: %v", err)
		return nil, errors.Database
	}

	return &pb.PreheatActivityResp{Success: true}, nil
}
