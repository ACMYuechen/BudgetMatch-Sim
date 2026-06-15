package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminSkuDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// SKU详情
func NewAdminSkuDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminSkuDetailLogic {
	return &AdminSkuDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminSkuDetailLogic) AdminSkuDetail(req *types.AdminSkuDetailReq) (resp *types.AdminSkuDetailResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.GetSku(l.ctx, &pb.GetSkuReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to get sku: %v", err)
		return nil, errors.ErrMallSkuNotFound
	}
	if rpcResp.Sku == nil {
		return nil, errors.ErrMallSkuNotFound
	}

	return &types.AdminSkuDetailResp{Sku: skuToType(rpcResp.Sku)}, nil
}
