package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminDeleteSkuLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 删除SKU
func NewAdminDeleteSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminDeleteSkuLogic {
	return &AdminDeleteSkuLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminDeleteSkuLogic) AdminDeleteSku(req *types.AdminDeleteSkuReq) error {
	_, err := l.svcCtx.MallProductClient.DeleteSku(l.ctx, &pb.DeleteSkuReq{Id: req.Id})
	if err != nil {
		l.Logger.Errorf("failed to delete sku: %v", err)
		return errors.Internal
	}
	return nil
}
