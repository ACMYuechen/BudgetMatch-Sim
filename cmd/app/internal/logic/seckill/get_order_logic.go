// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOrderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 查询订单
func NewGetOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderLogic {
	return &GetOrderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetOrderLogic) GetOrder(req *types.GetOrderReq) (resp *types.GetOrderResp, err error) {
	rpcResp, err := l.svcCtx.SeckillClient.GetOrder(l.ctx, &pb.GetOrderReq{
		OrderId: req.OrderId,
	})
	if err != nil {
		l.Logger.Errorf("failed to get order: %v", err)
		return nil, errors.SeckillOrderNotFound
	}

	return &types.GetOrderResp{
		OrderId:     rpcResp.OrderId,
		ActivityId:  rpcResp.ActivityId,
		SkuId:       rpcResp.SkuId,
		Quantity:    int64(rpcResp.Quantity),
		TotalAmount: rpcResp.TotalAmount,
		Status:      rpcResp.Status,
		CreatedAt:   rpcResp.CreatedAt,
	}, nil
}
