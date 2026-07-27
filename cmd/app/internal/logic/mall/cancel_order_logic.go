// Code scaffolded by goctl. No recover, Safe to edit.

package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CancelOrderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 取消订单
func NewCancelOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelOrderLogic {
	return &CancelOrderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CancelOrderLogic) CancelOrder(req *types.MallCancelOrderReq) (err error) {
	userId, err := authenticatedUserId(l.ctx)
	if err != nil {
		l.Logger.Errorf("return error: %v", err)
		return err
	}

	_, err = l.svcCtx.MallOrderClient.CancelOrder(l.ctx, &pb.CancelOrderReq{
		OrderId: req.Id,
		UserId:  userId,
	})
	if err != nil {
		l.Logger.Errorf("failed to cancel order: %v", err)
		return err
	}

	return nil
}
