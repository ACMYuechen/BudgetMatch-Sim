package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminCreateSkuLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建SKU
func NewAdminCreateSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminCreateSkuLogic {
	return &AdminCreateSkuLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminCreateSkuLogic) AdminCreateSku(req *types.AdminCreateSkuReq) (resp *types.AdminCreateSkuResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.CreateSku(l.ctx, &pb.CreateSkuReq{
		ProductId:    req.ProductId,
		Name:         req.Name,
		Specs:        req.Specs,
		Price:        req.Price,
		Stock:        req.Stock,
		AgentComment: req.AgentComment,
	})
	if err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, err
	}

	return &types.AdminCreateSkuResp{Id: rpcResp.Id}, nil
}
