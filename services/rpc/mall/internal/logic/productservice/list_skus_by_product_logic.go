package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type ListSkusByProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListSkusByProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSkusByProductLogic {
	return &ListSkusByProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListSkusByProductLogic) ListSkusByProduct(in *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error) {
	req := product_skus.ProductSkusListReq{
		Page:      int(in.Page),
		Size:      int(in.PageSize),
		ProductId: in.ProductId,
		Status:    int(in.Status),
	}
	if req.Status < 0 {
		req.Status = -1
	}

	list, total, err := l.svcCtx.SkuStore.List(l.ctx, req)
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, errors.Database
	}

	items := make([]*pb.Sku, 0, len(list))
	for i := range list {
		items = append(items, skuToPb(&list[i]))
	}

	return &pb.ListSkusByProductResp{
		List:     items,
		Total:    total,
		Page:     in.Page,
		PageSize: in.PageSize,
	}, nil
}
