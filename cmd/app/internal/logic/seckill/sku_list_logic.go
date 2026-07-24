// Code scaffolded by goctl. No recover, Safe to edit.

package seckill

import (
	"context"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/services/rpc/seckill/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SkuListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// SKU列表
func NewSkuListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SkuListLogic {
	return &SkuListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SkuListLogic) SkuList(req *types.SkuListReq) (resp *types.SkuListResp, err error) {
	rpcResp, err := l.svcCtx.SkuClient.ListSkusByActivity(l.ctx, &pb.ListSkusByActivityReq{
		ActivityId: req.ActivityId,
		Page:       int32(req.Page),
		PageSize:   int32(req.PageSize),
	})
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, err
	}

	items := make([]types.SkuItem, 0, len(rpcResp.List))
	for _, s := range rpcResp.List {
		items = append(items, types.SkuItem{
			Id:            s.Id,
			ActivityId:    s.ActivityId,
			Title:         s.Title,
			Subtitle:      s.Subtitle,
			Pic:           s.Pic,
			OriginalPrice: s.OriginalPrice,
			SeckillPrice:  s.SeckillPrice,
			Stock:         int64(s.Stock),
			Sold:          int64(s.Sold),
			Status:        s.Status,
			Sort:          int64(s.Sort),
		})
	}

	return &types.SkuListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
