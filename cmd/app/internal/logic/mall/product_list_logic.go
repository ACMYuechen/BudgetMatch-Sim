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

type ProductListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 商品列表
func NewProductListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProductListLogic {
	return &ProductListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProductListLogic) ProductList(req *types.MallProductListReq) (resp *types.MallProductListResp, err error) {
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

	items := make([]types.MallProductItem, 0, len(rpcResp.List))
	for _, p := range rpcResp.List {
		items = append(items, types.MallProductItem{
			Id:         p.Id,
			SpuCode:    p.SpuCode,
			Name:       p.Name,
			CategoryId: p.CategoryId,
			Brand:      p.Brand,
			Status:     p.Status,
			MainImage:  p.MainImage,
			Detail:     p.Detail,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}

	return &types.MallProductListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
