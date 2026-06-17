package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminUpdateSkuLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新SKU
func NewAdminUpdateSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminUpdateSkuLogic {
	return &AdminUpdateSkuLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminUpdateSkuLogic) AdminUpdateSku(req *types.AdminUpdateSkuReq) (resp *types.AdminUpdateSkuResp, err error) {
	_, err = l.svcCtx.MallProductClient.UpdateSku(l.ctx, &pb.UpdateSkuReq{
		Id:     req.Id,
		Name:   req.Name,
		Specs:  req.Specs,
		Price:  req.Price,
		Stock:  req.Stock,
		Status: req.Status,
	})
	if err != nil {
		l.Logger.Errorf("failed to update sku: %v", err)
		return nil, errors.Internal
	}

	return &types.AdminUpdateSkuResp{Success: true}, nil
}
