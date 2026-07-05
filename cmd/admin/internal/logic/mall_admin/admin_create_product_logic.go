package mall_admin

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/cmd/admin/internal/svc"
	"budgetmatch-sim/cmd/admin/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type AdminCreateProductLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// 创建商品
func NewAdminCreateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminCreateProductLogic {
	return &AdminCreateProductLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminCreateProductLogic) AdminCreateProduct(req *types.AdminCreateProductReq) (resp *types.AdminCreateProductResp, err error) {
	rpcResp, err := l.svcCtx.MallProductClient.CreateProduct(l.ctx, &pb.CreateProductReq{
		UserId:       req.UserId,
		Name:         req.Name,
		Content:      req.Content,
		Image:        req.Image,
		Providor:     req.Providor,
		AgentComment: req.AgentComment,
	})
	if err != nil {
		l.Logger.Errorf("failed to create product: %v", err)
		return nil, errors.Internal
	}

	return &types.AdminCreateProductResp{Id: rpcResp.Id}, nil
}
