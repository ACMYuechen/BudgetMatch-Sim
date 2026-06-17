package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminUpdateProductLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 更新商品
func NewAdminUpdateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminUpdateProductLogic {
	return &AdminUpdateProductLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminUpdateProductLogic) AdminUpdateProduct(req *types.AdminUpdateProductReq) (resp *types.AdminUpdateProductResp, err error) {
	_, err = l.svcCtx.MallProductClient.UpdateProduct(l.ctx, &pb.UpdateProductReq{
		Id:         req.Id,
		Name:       req.Name,
		CategoryId: req.CategoryId,
		Brand:      req.Brand,
		Status:     req.Status,
		MainImage:  req.MainImage,
		Detail:     req.Detail,
	})
	if err != nil {
		l.Logger.Errorf("failed to update product: %v", err)
		return nil, errors.Internal
	}

	return &types.AdminUpdateProductResp{Success: true}, nil
}
