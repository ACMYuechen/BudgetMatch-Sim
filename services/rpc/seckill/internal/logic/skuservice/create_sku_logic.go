package skuservicelogic

import (
	"context"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

type CreateSkuLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateSkuLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSkuLogic {
	return &CreateSkuLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *CreateSkuLogic) CreateSku(in *pb.CreateSkuReq) (*pb.CreateSkuResp, error) {
	// verify activity exists
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.ActivityId)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.ErrDatabase
	}
	if activity == nil {
		return nil, errors.ErrSeckillActivityNotFound
	}

	if in.Stock < 0 {
		return nil, errors.ErrInternal
	}

	sku := &seckill_sku.SeckillSkus{
		Id:            uuid.New().String(),
		ActivityId:    in.ActivityId,
		Title:         in.Title,
		Subtitle:      in.Subtitle,
		Pic:           in.Pic,
		OriginalPrice: in.OriginalPrice,
		SeckillPrice:  in.SeckillPrice,
		Stock:         in.Stock,
		Sold:          0,
		LockStock:     0,
		Status:        1,
		Sort:          in.Sort,
	}

	if err := l.svcCtx.SkuStore.InsertOne(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, errors.ErrDatabase
	}

	return &pb.CreateSkuResp{Id: sku.Id}, nil
}
