package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminDeleteProductLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除商品
func NewAdminDeleteProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminDeleteProductLogic {
	return &AdminDeleteProductLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminDeleteProductLogic) AdminDeleteProduct(req *types.AdminDeleteProductReq) error {
	_, err := l.svcCtx.MallProductClient.DeleteProduct(l.ctx, &pb.DeleteProductReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to delete product: %v", err)
		return errors.Internal
	}
	return nil
}
