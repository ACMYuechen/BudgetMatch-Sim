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

func (l *CancelOrderLogic) CancelOrder(req *types.MallCancelOrderReq) error {
	userID := l.ctx.Value("user_id")
	if userID == nil {
		l.Logger.Errorf("return error: %v", errors.Unauthorized)
		return errors.Unauthorized
	}

	_, err := l.svcCtx.MallOrderClient.CancelOrder(l.ctx, &pb.CancelOrderReq{
		OrderId: req.Id,
		UserId:  userID.(string),
	})
	if err != nil {
		l.Logger.Errorf("failed to cancel order: %v", err)
		return err
	}

	return nil
}
