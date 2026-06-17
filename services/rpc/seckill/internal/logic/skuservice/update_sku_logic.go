package skuservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type UpdateSkuLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSkuLogic {
	return &UpdateSkuLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateSkuLogic) UpdateSku(in *pb.UpdateSkuReq) (*pb.UpdateSkuResp, error) {
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil {
		return nil, errors.SeckillSkuNotFound
	}

	if in.Title != "" {
		sku.Title = in.Title
	}
	if in.Subtitle != "" {
		sku.Subtitle = in.Subtitle
	}
	if in.Pic != "" {
		sku.Pic = in.Pic
	}
	if in.OriginalPrice > 0 {
		sku.OriginalPrice = in.OriginalPrice
	}
	if in.SeckillPrice > 0 {
		sku.SeckillPrice = in.SeckillPrice
	}
	if in.Stock >= 0 {
		sku.Stock = in.Stock
	}
	if in.Sort >= 0 {
		sku.Sort = in.Sort
	}
	if in.Status >= 0 {
		sku.Status = int64(in.Status)
	}

	if err := l.svcCtx.SkuStore.Update(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to update sku: %v", err)
		return nil, errors.Database
	}

	return &pb.UpdateSkuResp{Success: true}, nil
}
