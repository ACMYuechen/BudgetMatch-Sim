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

type ProductDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 商品详情
func NewProductDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProductDetailLogic {
	return &ProductDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProductDetailLogic) ProductDetail(req *types.MallProductDetailReq) (resp *types.MallProductDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.GetProduct(l.ctx, &pb.GetProductReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to get product: %v", err)
		return nil, errors.MallProductNotFound
	}
	if rpcResp.Product == nil {
		return nil, errors.MallProductNotFound
	}

	p := rpcResp.Product
	return &types.MallProductDetailResp{
		Product: types.MallProductItem{
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
		},
	}, nil
}
