package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminProductDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 商品详情
func NewAdminProductDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminProductDetailLogic {
	return &AdminProductDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminProductDetailLogic) AdminProductDetail(req *types.AdminProductDetailReq) (resp *types.AdminProductDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.GetProduct(l.ctx, &pb.GetProductReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to get product: %v", err)
		return nil, errors.MallProductNotFound
	}
	if rpcResp.Product == nil {
		l.Logger.Errorf("return error: %v", errors.MallProductNotFound)
		return nil, errors.MallProductNotFound
	}

	return &types.AdminProductDetailResp{Product: productToType(rpcResp.Product)}, nil
}
