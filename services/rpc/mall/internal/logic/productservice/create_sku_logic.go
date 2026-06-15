package productservicelogic

import (
	"context"

	"github.com/google/uuid"
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
	// validate product exists
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, in.ProductId)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.ErrDatabase
	}
	if product == nil {
		return nil, errors.ErrMallProductNotFound
	}

	// check duplicate sku_code under product
	existing, err := l.svcCtx.SkuStore.FindBySkuCode(l.ctx, in.ProductId, in.SkuCode)
	if err != nil {
		l.Logger.Errorf("failed to find sku by code: %v", err)
		return nil, errors.ErrDatabase
	}
	if existing != nil {
		return nil, errors.ErrInternal
	}

	sku := &product_skus.ProductSkus{
		Id:        uuid.New().String(),
		ProductId: in.ProductId,
		SkuCode:   in.SkuCode,
		Name:      in.Name,
		Specs:     in.Specs,
		Price:     in.Price,
		Stock:     in.Stock,
		Sold:      0,
		Status:    1,
	}

	if err := l.svcCtx.SkuStore.InsertOne(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, errors.ErrDatabase
	}

	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(in.ProductId))

	return &pb.CreateSkuResp{Id: sku.Id}, nil
}
