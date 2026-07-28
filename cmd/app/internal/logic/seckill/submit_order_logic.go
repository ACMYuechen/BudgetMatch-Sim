// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SubmitOrderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 提交秒杀订单
func NewSubmitOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubmitOrderLogic {
	return &SubmitOrderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubmitOrderLogic) SubmitOrder(req *types.SubmitOrderReq) (resp *types.SubmitOrderResp, err error) {
	userID := request.UserID(l.ctx)
	if userID == "" {
		l.Logger.Errorf("return error: %v", errors.Unauthorized)
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.SeckillClient.SubmitOrder(l.ctx, &pb.SubmitOrderReq{
		ActivityId: req.ActivityId,
		SkuId:      req.SkuId,
		UserId:     userID,
		Token:      req.Token,
		Quantity:   int32(req.Quantity),
	})
	if err != nil {
		l.Logger.Errorf("failed to submit order: %v", err)
		return nil, err
	}

	return &types.SubmitOrderResp{
		OrderId: rpcResp.OrderId,
		Status:  rpcResp.Status,
	}, nil
}
