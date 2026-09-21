package productindexservicelogic

import (
	"context"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListProductIndexLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListProductIndexLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListProductIndexLogic {
	return &ListProductIndexLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListProductIndexLogic) ListProductIndex(in *pb.ListProductIndexReq) (*pb.ListProductIndexResp, error) {
	if l.ctx.Value(interceptor.ContextKeyServiceName) != serviceauth.ServiceAgent ||
		l.ctx.Value(interceptor.ContextKeyServicePurpose) != serviceauth.PurposeProductIndexRead {
		return nil, errors.Unauthorized
	}
	if in == nil || !product_index.ValidCursor(in.Cursor) || in.PageSize < 0 || in.PageSize > product_index.MaxPageSize {
		return nil, errors.Invalid
	}
	size := int(in.PageSize)
	if size == 0 {
		size = product_index.DefaultPageSize
	}
	if l.svcCtx.ProductIndexStore == nil {
		return nil, errors.Internal
	}
	rows, err := l.svcCtx.ProductIndexStore.ListPage(l.ctx, in.Cursor, size+1)
	if err != nil {
		// Do not log SQL, raw upstream errors, credentials or catalog contents.
		l.Logger.Error("product index page read failed")
		return nil, errors.Database
	}
	resp := &pb.ListProductIndexResp{Complete: len(rows) <= size}
	if !resp.Complete {
		rows = rows[:size]
		resp.NextCursor = rows[len(rows)-1].SkuId
	}
	for _, row := range rows {
		resp.List = append(resp.List, &pb.ProductIndexEntry{
			SkuId: row.SkuId, ProductId: row.ProductId, ProductName: row.ProductName,
			ProductContent: row.ProductContent, Provider: row.Provider,
			SkuName: row.SkuName, Specs: row.Specs, Price: row.Price, Stock: row.Stock, Sold: row.Sold,
		})
	}
	return resp, nil
}
