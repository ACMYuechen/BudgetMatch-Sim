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
	userID := l.ctx.Value("user_id")
	if userID == nil {
		l.Logger.Errorf("return error: %v", errors.Unauthorized)
		return nil, errors.Unauthorized
	}

	rpcResp, err := l.svcCtx.MallOrderClient.CreateOrder(l.ctx, &pb.CreateOrderReq{
		UserId:         userID.(string),
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
