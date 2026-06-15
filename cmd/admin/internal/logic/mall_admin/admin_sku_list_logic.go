package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminSkuListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// SKU列表
func NewAdminSkuListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminSkuListLogic {
	return &AdminSkuListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminSkuListLogic) AdminSkuList(req *types.AdminSkuListReq) (resp *types.AdminSkuListResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.ListSkusByProduct(l.ctx, &pb.ListSkusByProductReq{
		ProductId: req.ProductId,
		Page:      int32(req.Page),
		PageSize:  int32(req.PageSize),
		Status:    int32(req.Status),
	})
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, errors.ErrDatabase
	}

	items := make([]types.AdminSkuItem, 0, len(rpcResp.List))
	for _, s := range rpcResp.List {
		items = append(items, skuToType(s))
	}

	return &types.AdminSkuListResp{
		List:     items,
		Total:    rpcResp.Total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}
