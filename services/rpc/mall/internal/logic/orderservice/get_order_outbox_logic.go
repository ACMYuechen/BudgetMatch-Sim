package orderservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOrderOutboxLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetOrderOutboxLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderOutboxLogic {
	return &GetOrderOutboxLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetOrderOutboxLogic) GetOrderOutbox(in *pb.GetOrderOutboxReq) (*pb.GetOrderOutboxResp, error) {
	if in == nil || in.Id == "" {
		return nil, errors.Invalid
	}
	event, err := l.svcCtx.OrderOutboxStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to get order outbox event: event_id=%s error=%v", in.Id, err)
		return nil, errors.Database
	}
	if event == nil {
		return nil, errors.NotFound
	}
	return &pb.GetOrderOutboxResp{Event: outboxEventToPb(event)}, nil
}
