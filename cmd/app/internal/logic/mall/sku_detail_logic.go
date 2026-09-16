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

type SkuDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// SKU详情，用于推荐结果定位商品
func NewSkuDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuDetailLogic {
	return &SkuDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuDetailLogic) SkuDetail(req *types.MallSkuDetailReq) (resp *types.MallSkuDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.GetSku(l.ctx, &pb.GetSkuReq{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if rpcResp == nil || rpcResp.Sku == nil {
		return nil, errors.MallSkuNotFound
	}
	sku := rpcResp.Sku
	return &types.MallSkuDetailResp{Sku: types.MallSkuItem{
		Id: sku.Id, ProductId: sku.ProductId, Name: sku.Name, Specs: sku.Specs,
		Price: sku.Price, Stock: sku.Stock, Sold: sku.Sold, Status: sku.Status,
		AgentComment: sku.AgentComment, CreatedAt: sku.CreatedAt, UpdatedAt: sku.UpdatedAt,
	}}, nil
}
