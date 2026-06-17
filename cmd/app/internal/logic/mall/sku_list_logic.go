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

func (l *SkuListLogic) SkuList(req *types.MallSkuListReq) (resp *types.MallSkuListResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.ListSkusByProduct(l.ctx, &pb.ListSkusByProductReq{
		ProductId: req.ProductId,
		Page:      int32(req.Page),
		PageSize:  int32(req.PageSize),
		Status:    int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, errors.Database
	}

	items := make([]types.MallSkuItem, 0, len(rpcResp.List))
	for _, s := range rpcResp.List {
		items = append(items, types.MallSkuItem{
			Id:        s.Id,
			ProductId: s.ProductId,
			SkuCode:   s.SkuCode,
			Name:      s.Name,
			Specs:     s.Specs,
			Price:     s.Price,
			Stock:     s.Stock,
			Sold:      s.Sold,
			Status:    s.Status,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		})
	}

	return &types.MallSkuListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
