package skuservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type ListSkusByActivityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListSkusByActivityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListSkusByActivityLogic {
	return &ListSkusByActivityLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListSkusByActivityLogic) ListSkusByActivity(in *pb.ListSkusByActivityReq) (*pb.ListSkusByActivityResp, error) {
	page := int(in.Page)
	if page <= 0 {
		page = 1
	}
	pageSize := int(in.PageSize)
	if pageSize <= 0 {
		pageSize = 20
	}

	skus, total, err := l.svcCtx.SkuStore.ListByActivity(l.ctx, in.ActivityId, page, pageSize)
	if err != nil {
		l.Logger.Errorf("failed to list skus: %v", err)
		return nil, errors.Database
	}

	list := make([]*pb.Sku, 0, len(skus))
	for _, s := range skus {
		list = append(list, skuToPb(&s))
	}

	return &pb.ListSkusByActivityResp{
		List:      list,
		Total:     total,
		Page:      int32(page),
		PageSize:  int32(pageSize),
	}, nil
}
