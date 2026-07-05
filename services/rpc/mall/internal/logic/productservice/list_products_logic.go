package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/products"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type ListProductsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListProductsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListProductsLogic {
	return &ListProductsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListProductsLogic) ListProducts(in *pb.ListProductsReq) (*pb.ListProductsResp, error) {
	req := products.ProductsListReq{
		Page:    int(in.Page),
		Size:    int(in.PageSize),
		UserId:  in.UserId,
		Keyword: in.Keyword,
		Status:  in.Status,
	}
	if req.Status < 0 {
		req.Status = -1
	}

	list, total, err := l.svcCtx.ProductStore.List(l.ctx, req)
	if err != nil {
		l.Logger.Errorf("failed to list products: %v", err)
		return nil, errors.Database
	}

	items := make([]*pb.Product, 0, len(list))
	for i := range list {
		items = append(items, productToPb(&list[i]))
	}

	return &pb.ListProductsResp{
		List:     items,
		Total:    total,
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}
