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
		return nil, err
	}
	if rpcResp.Product == nil {
		l.Logger.Errorf("return error: %v", errors.MallProductNotFound)
		return nil, errors.MallProductNotFound
	}

	p := rpcResp.Product
	return &types.MallProductDetailResp{
		Product: types.MallProductItem{
			Id:           p.Id,
			UserId:       p.UserId,
			Name:         p.Name,
			Content:      p.Content,
			Image:        p.Image,
			Providor:     p.Providor,
			Status:       p.Status,
			AgentComment: p.AgentComment,
			CreatedAt:    p.CreatedAt,
			UpdatedAt:    p.UpdatedAt,
		},
	}, nil
}
