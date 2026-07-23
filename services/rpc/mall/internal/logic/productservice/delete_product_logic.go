package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type DeleteProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteProductLogic {
	return &DeleteProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteProductLogic) DeleteProduct(in *pb.DeleteProductReq) (*pb.DeleteProductResp, error) {
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil {
		l.Logger.Errorf("return error: %v", errors.MallProductNotFound)
		return nil, errors.MallProductNotFound
	}

	if err := l.svcCtx.ProductStore.Delete(l.ctx, in.Id); err != nil {
		l.Logger.Errorf("failed to delete product: %v", err)
		return nil, errors.Database
	}

	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(in.Id))

	return &pb.DeleteProductResp{Success: true}, nil
}
