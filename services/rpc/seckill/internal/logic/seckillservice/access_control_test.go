package seckillservicelogic

import (
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSeckillRejectsSpoofedUserBeforeIO(t *testing.T) {
	for _, role := range []int64{2, 100} {
		ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "caller")
		ctx = context.WithValue(ctx, interceptor.ContextKeyRole, role)
		for _, ctx := range []context.Context{context.Background(), ctx} {
			_, err := NewAcquireTokenLogic(ctx, &svc.ServiceContext{}).AcquireToken(&pb.AcquireTokenReq{UserId: "victim", ActivityId: "activity", SkuId: "sku"})
			require.ErrorIs(t, err, errors.Unauthorized)
			_, err = NewSubmitOrderLogic(ctx, &svc.ServiceContext{}).SubmitOrder(&pb.SubmitOrderReq{UserId: "victim", ActivityId: "activity", SkuId: "sku", Token: "victim-token"})
			require.ErrorIs(t, err, errors.Unauthorized)
		}
	}
}
