package orderservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOrderOutboxStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetOrderOutboxStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderOutboxStatsLogic {
	return &GetOrderOutboxStatsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetOrderOutboxStatsLogic) GetOrderOutboxStats(in *pb.GetOrderOutboxStatsReq) (*pb.GetOrderOutboxStatsResp, error) {
	counts, oldestPending, err := l.svcCtx.OrderOutboxStore.GetStats(l.ctx)
	if err != nil {
		l.Logger.Errorf("failed to get order outbox stats: %v", err)
		return nil, errors.Database
	}
	resp := &pb.GetOrderOutboxStatsResp{Counts: make([]*pb.OrderOutboxStatusCount, 0, len(counts))}
	for _, count := range counts {
		resp.Counts = append(resp.Counts, &pb.OrderOutboxStatusCount{Status: int32(count.Status), EventType: count.EventType, Count: count.Count})
	}
	if !oldestPending.IsZero() {
		resp.OldestPendingAt = oldestPending.Unix()
	}
	return resp, nil
}
