package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminProductListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 商品列表
func NewAdminProductListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminProductListLogic {
	return &AdminProductListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminProductListLogic) AdminProductList(req *types.AdminProductListReq) (resp *types.AdminProductListResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.ListProducts(l.ctx, &pb.ListProductsReq{
		Page:       int32(req.Page),
		PageSize:   int32(req.PageSize),
		CategoryId: req.CategoryId,
		Keyword:    req.Keyword,
		Status:     int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list products: %v", err)
		return nil, errors.Database
	}

	items := make([]types.AdminProductItem, 0, len(rpcResp.List))
	for _, p := range rpcResp.List {
		items = append(items, productToType(p))
	}

	return &types.AdminProductListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
