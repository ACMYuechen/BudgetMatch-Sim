package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
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
		l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
		return nil, errors.MallSkuNotFound
	}

	if in.Name != "" {
		sku.Name = in.Name
	}
	if in.Specs != "" {
		sku.Specs = in.Specs
	}
	if in.Price > 0 {
		sku.Price = in.Price
	}
	if in.Stock >= 0 {
		sku.Stock = int(in.Stock)
	}
	if in.Status >= 0 {
		sku.Status = int(in.Status)
	}
	if in.AgentComment != "" {
		sku.AgentComment = in.AgentComment
	}

	if err := l.svcCtx.SkuStore.Update(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to update sku: %v", err)
		return nil, errors.Database
	}

	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(sku.ProductId))

	return &pb.UpdateSkuResp{Success: true}, nil
}
