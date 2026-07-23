package productservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type GetSkuLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSkuLogic {
	return &GetSkuLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetSkuLogic) GetSku(in *pb.GetSkuReq) (*pb.GetSkuResp, error) {
	sku, err := l.svcCtx.SkuStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find sku: %v", err)
		return nil, errors.Database
	}
	if sku == nil {
		l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
		return nil, errors.MallSkuNotFound
	}

	return &pb.GetSkuResp{Sku: skuToPb(sku)}, nil
}
