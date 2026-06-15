package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type DeleteSkuLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteSkuLogic {
	return &DeleteSkuLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteSkuLogic) DeleteSku(in *pb.DeleteSkuReq) (*pb.DeleteSkuResp, error) {
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.ErrDatabase
	}
	if sku == nil {
		return nil, errors.ErrMallSkuNotFound
	}

	if err := l.svcCtx.SkuStore.Delete(l.ctx, in.Id); err != nil {
		l.Logger.Errorf("failed to delete sku: %v", err)
		return nil, errors.ErrDatabase
	}

	_ = l.svcCtx.Redis.Del(l.ctx, productCacheKey(sku.ProductId))

	return &pb.DeleteSkuResp{Success: true}, nil
}
