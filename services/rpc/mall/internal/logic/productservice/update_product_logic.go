package productservicelogic

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type UpdateProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateProductLogic {
	return &UpdateProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *UpdateProductLogic) UpdateProduct(in *pb.UpdateProductReq) (*pb.UpdateProductResp, error) {
	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil {
		return nil, errors.MallProductNotFound
	}

	if in.Name != "" {
		product.Name = in.Name
	}
	if in.Content != "" {
		product.Content = in.Content
	}
	if in.Providor != "" {
		product.Providor = in.Providor
	}
	if in.Status >= 0 {
		product.Status = in.Status
	}
	if in.Image != "" {
		product.Image = in.Image
	}
	if in.AgentComment != "" {
		product.AgentComment = in.AgentComment
	}

	if err := l.svcCtx.ProductStore.Update(l.ctx, product); err != nil {
		l.Logger.Errorf("failed to update product: %v", err)
		return nil, errors.Database
	}

	// invalidate cache
	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(in.Id))

	return &pb.UpdateProductResp{Success: true}, nil
}

func productCacheKey(productId string) string {
	return fmt.Sprintf("mall:product:%s", productId)
}
