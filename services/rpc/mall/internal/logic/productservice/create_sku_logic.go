package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type CreateSkuLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSkuLogic {
	return &CreateSkuLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateSkuLogic) CreateSku(in *pb.CreateSkuReq) (*pb.CreateSkuResp, error) {
	// 校验商品是否存在
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, in.ProductId)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil {
		return nil, errors.MallProductNotFound
	}

	sku := &product_skus.ProductSkus{
		ProductId:    in.ProductId,
		Name:         in.Name,
		Specs:        in.Specs,
		Price:        in.Price,
		Stock:        int(in.Stock),
		Sold:         0,
		Status:       1,
		AgentComment: in.AgentComment,
	}

	if err := l.svcCtx.SkuStore.InsertOne(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, errors.Database
	}

	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(in.ProductId))

	return &pb.CreateSkuResp{Id: sku.Id}, nil
}
