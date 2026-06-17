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
	userID := l.ctx.Value("user_id")
	if userID == nil {
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.SeckillClient.SubmitOrder(l.ctx, &pb.SubmitOrderReq{
		ActivityId: req.ActivityId,
		SkuId:      req.SkuId,
		UserId:     userID.(string),
		Token:      req.Token,
		Quantity:   req.Quantity,
	})
	if err != nil {
		l.Logger.Errorf("failed to submit order: %v", err)
		return nil, errors.SeckillSubmitFailed
	}

	return &types.SubmitOrderResp{
		OrderId: rpcResp.OrderId,
		Status:  rpcResp.Status,
	}, nil
}
