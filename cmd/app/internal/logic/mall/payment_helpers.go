package mall

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/infra/errors"
	mallpb "budgetmatch-sim/services/rpc/mall/pb"
)

const MallOrderStatusPending int32 = 1

func authenticatedUserID(ctx context.Context) (string, error) {
	userID, ok := ctx.Value("user_id").(string)
	if !ok || userID == "" {
		return "", errors.Unauthorized
	}
	return userID, nil
}

func loadPaymentOrder(ctx context.Context, svcCtx *svc.ServiceContext, orderID, userID string) (*mallpb.Order, error) {
	rpcResp, err := svcCtx.MallOrderClient.GetOrder(ctx, &mallpb.GetOrderReq{
		OrderId: orderID,
		UserId:  userID,
	})
	if err != nil {
		return nil, err
	}
	if rpcResp == nil || rpcResp.Order == nil {
		return nil, errors.MallOrderNotFound
	}

	order := rpcResp.Order
	if order.UserId != userID {
		return nil, errors.MallOrderNotFound
	}
	if order.OriginalAmount < 0 ||
		order.DiscountAmount < 0 ||
		order.PayAmount <= 0 ||
		order.PayAmount != order.OriginalAmount-order.DiscountAmount {
		return nil, errors.Invalid
	}
	return order, nil
}
