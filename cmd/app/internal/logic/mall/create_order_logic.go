// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateOrderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建订单
func NewCreateOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrderLogic {
	return &CreateOrderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateOrderLogic) CreateOrder(req *types.MallCreateOrderReq) (resp *types.MallCreateOrderResp, err error) {
	userID, err := authenticatedUserID(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return nil, err
	}

	rpcResp, err := l.svcCtx.MallOrderClient.CreateOrder(l.ctx, &pb.CreateOrderReq{
		UserId:         userID,
		SkuId:          req.SkuId,
		Quantity:       req.Quantity,
		Remark:         req.Remark,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		l.Logger.Errorf("failed to create order: %v", err)
		return nil, err
	}

	return &types.MallCreateOrderResp{
		OrderId: rpcResp.OrderId,
		Status:  rpcResp.Status,
	}, nil
}
