package orderservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_order_outbox"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListOrderOutboxLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListOrderOutboxLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListOrderOutboxLogic {
	return &ListOrderOutboxLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListOrderOutboxLogic) ListOrderOutbox(in *pb.ListOrderOutboxReq) (*pb.ListOrderOutboxResp, error) {
	if in == nil {
		return nil, errors.Invalid
	}
	page, pageSize := int(in.Page), int(in.PageSize)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	list, total, err := l.svcCtx.OrderOutboxStore.ListFiltered(l.ctx, mall_order_outbox.ListFilteredReq{
		Page: page, Size: pageSize, Status: int(in.Status), EventType: in.EventType, AggregateId: in.AggregateId, DedupKey: in.DedupKey,
	})
	if err != nil {
		l.Logger.Errorf("failed to list order outbox events: %v", err)
		return nil, errors.Database
	}
	resp := &pb.ListOrderOutboxResp{List: make([]*pb.OrderOutboxEvent, 0, len(list)), Total: total, Page: int32(page), PageSize: int32(pageSize)}
	for i := range list {
		resp.List = append(resp.List, outboxEventToPb(&list[i]))
	}
	return resp, nil
}
