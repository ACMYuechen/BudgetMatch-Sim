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

type SkuUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新SKU
func NewSkuUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuUpdateLogic {
	return &SkuUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuUpdateLogic) SkuUpdate(req *types.SkuUpdateReq) (resp *types.SkuUpdateResp, err error) {
	rpcResp, err := l.svcCtx.SkuClient.UpdateSku(l.ctx, &pb.UpdateSkuReq{
		Id:            req.Id,
		Title:         req.Title,
		Subtitle:      req.Subtitle,
		Pic:           req.Pic,
		OriginalPrice: req.OriginalPrice,
		SeckillPrice:  req.SeckillPrice,
		Stock:         req.Stock,
		Sort:          req.Sort,
		Status:        req.Status,
	})
	if err != nil {
		l.Logger.Errorf("failed to update sku: %v", err)
		return nil, errors.ErrDatabase
	}

	return &types.SkuUpdateResp{
		Success: rpcResp.Success,
	}, nil
}
