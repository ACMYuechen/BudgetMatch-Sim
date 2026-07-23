// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type OrderListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 订单列表
func NewOrderListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrderListLogic {
	return &OrderListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OrderListLogic) OrderList(req *types.MallOrderListReq) (resp *types.MallOrderListResp, err error) {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		l.Logger.Errorf("return error: %v", errors.Unauthorized)
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.MallOrderClient.ListOrders(l.ctx, &pb.ListOrdersReq{
		UserId:   userID.(string),
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
		Status:   int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list orders: %v", err)
		return nil, errors.Database
	}

	items := make([]types.MallOrderResp, 0, len(rpcResp.List))
	for _, o := range rpcResp.List {
		items = append(items, orderToType(o))
	}

	return &types.MallOrderListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

func orderToType(o *pb.Order) types.MallOrderResp {
	items := make([]types.MallOrderItem, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, types.MallOrderItem{
			ProductId:      it.ProductId,
			SkuId:          it.SkuId,
			SkuName:        it.SkuName,
			Price:          it.Price,
			Quantity:       it.Quantity,
			DiscountAmount: it.DiscountAmount,
			TotalAmount:    it.TotalAmount,
			Snapshot:       it.Snapshot,
		})
	}
	return types.MallOrderResp{
		Id:             o.Id,
		UserId:         o.UserId,
		OriginalAmount: o.OriginalAmount,
		DiscountAmount: o.DiscountAmount,
		PayAmount:      o.PayAmount,
		Status:         o.Status,
		PayType:        o.PayType,
		PayTime:        o.PayTime,
		Remark:         o.Remark,
		Snapshot:       o.Snapshot,
		IdempotencyKey: o.IdempotencyKey,
		Items:          items,
		CreatedAt:      o.CreatedAt,
		UpdatedAt:      o.UpdatedAt,
	}
}
