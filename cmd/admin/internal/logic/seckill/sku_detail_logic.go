// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SkuDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// SKU详情
func NewSkuDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuDetailLogic {
	return &SkuDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuDetailLogic) SkuDetail(req *types.SkuDetailReq) (resp *types.SkuDetailResp, err error) {
	rpcResp, err := l.svcCtx.SkuClient.GetSku(l.ctx, &pb.GetSkuReq{
		Id: req.Id,
	})
	if err != nil {
		l.Logger.Errorf("failed to get sku: %v", err)
		return nil, errors.ErrSeckillSkuNotFound
	}

	sku := rpcResp.Sku
	return &types.SkuDetailResp{
		Sku: types.SkuItem{
			Id:            sku.Id,
			ActivityId:    sku.ActivityId,
			Title:         sku.Title,
			Subtitle:      sku.Subtitle,
			Pic:           sku.Pic,
			OriginalPrice: sku.OriginalPrice,
			SeckillPrice:  sku.SeckillPrice,
			Stock:         sku.Stock,
			Sold:          sku.Sold,
			LockStock:     sku.LockStock,
			Status:        sku.Status,
			Sort:          sku.Sort,
			CreatedAt:     sku.CreatedAt,
			UpdatedAt:     sku.UpdatedAt,
		},
	}, nil
}
