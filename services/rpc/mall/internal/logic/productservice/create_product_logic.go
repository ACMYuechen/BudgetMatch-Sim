package productservicelogic

import (
	"context"

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
	product := &products.Products{
		UserId:       in.UserId,
		Name:         in.Name,
		Content:      in.Content,
		Image:        in.Image,
		Providor:     in.Providor,
		Status:       1,
		AgentComment: in.AgentComment,
	}

	if err := l.svcCtx.ProductStore.InsertOne(l.ctx, product); err != nil {
		l.Logger.Errorf("failed to create product: %v", err)
		return nil, errors.Database
	}

	return &pb.CreateProductResp{Id: product.Id}, nil
}
