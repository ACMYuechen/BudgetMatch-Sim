package orderservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type ListOrdersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListOrdersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListOrdersLogic {
	return &ListOrdersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListOrdersLogic) ListOrders(in *pb.ListOrdersReq) (*pb.ListOrdersResp, error) {
	paymentStatus := int(in.PaymentStatus)
	if paymentStatus < -1 || paymentStatus > int(pb.PaymentStatus_PAYMENT_STATUS_ABNORMAL) {
		l.Logger.Errorf("invalid payment status: %d", paymentStatus)
		return nil, errors.Invalid
	}

	req := mall_orders.MallOrdersListReq{
		Page:          int(in.Page),
		Size:          int(in.PageSize),
		UserId:        in.UserId,
		Status:        int(in.Status),
		PaymentStatus: paymentStatus,
	}
	if req.Status < 0 {
		req.Status = -1
	}

	list, total, err := l.svcCtx.OrderStore.List(l.ctx, req)
	if err != nil {
		l.Logger.Errorf("failed to list orders: payment_status=%d error=%v", paymentStatus, err)
		return nil, errors.Database
	}

	items := make([]*pb.Order, 0, len(list))
	for i := range list {
		orderItems, err := l.svcCtx.OrderItemStore.FindByOrderId(l.ctx, list[i].Id)
		if err != nil {
			l.Logger.Errorf("failed to find order items: %v", err)
			return nil, errors.Database
		}
		items = append(items, orderToPb(&list[i], orderItems))
	}

	return &pb.ListOrdersResp{
		List:     items,
		Total:    total,
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}
