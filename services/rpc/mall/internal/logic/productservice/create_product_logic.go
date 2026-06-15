package productservicelogic

import (
	"context"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/products"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type CreateProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateProductLogic {
	return &CreateProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateProductLogic) CreateProduct(in *pb.CreateProductReq) (*pb.CreateProductResp, error) {
	// check duplicate spu_code
	existing, err := l.svcCtx.ProductStore.FindBySpuCode(l.ctx, in.SpuCode)
	if err != nil {
		l.Logger.Errorf("failed to find product by spu_code: %v", err)
		return nil, errors.ErrDatabase
	}
	if existing != nil {
		l.Logger.Errorf("spu_code already exists: %s", in.SpuCode)
		return nil, errors.ErrInternal
	}

	product := &products.Products{
		Id:         uuid.New().String(),
		SpuCode:    in.SpuCode,
		Name:       in.Name,
		CategoryId: in.CategoryId,
		Brand:      in.Brand,
		Status:     1,
		MainImage:  in.MainImage,
		Detail:     in.Detail,
	}

	if err := l.svcCtx.ProductStore.InsertOne(l.ctx, product); err != nil {
		l.Logger.Errorf("failed to create product: %v", err)
		return nil, errors.ErrDatabase
	}

	return &pb.CreateProductResp{Id: product.Id}, nil
}
