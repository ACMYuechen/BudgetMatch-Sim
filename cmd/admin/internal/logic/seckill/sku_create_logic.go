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

type SkuCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建SKU
func NewSkuCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuCreateLogic {
	return &SkuCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuCreateLogic) SkuCreate(req *types.SkuCreateReq) (resp *types.SkuCreateResp, err error) {
	rpcResp, err := l.svcCtx.SkuClient.CreateSku(l.ctx, &pb.CreateSkuReq{
		ActivityId:    req.ActivityId,
		Title:         req.Title,
		Subtitle:      req.Subtitle,
		Pic:           req.Pic,
		OriginalPrice: req.OriginalPrice,
		SeckillPrice:  req.SeckillPrice,
		Stock:         int32(req.Stock),
		Sort:          int32(req.Sort),
		MallSkuId:     req.MallSkuId,
	})
	if err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, errors.Database
	}

	return &types.SkuCreateResp{
		Id: rpcResp.Id,
	}, nil
}
