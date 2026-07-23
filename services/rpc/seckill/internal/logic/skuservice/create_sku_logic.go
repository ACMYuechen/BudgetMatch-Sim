package skuservicelogic

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
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
	// 校验活动是否存在
	activity, err := l.svcCtx.ActivityStore.FindOne(l.ctx, in.ActivityId)
	if err != nil {
		l.Logger.Errorf("failed to find activity: %v", err)
		return nil, errors.Database
	}
	if activity == nil {
		l.Logger.Errorf("return error: %v", errors.SeckillActivityNotFound)
		return nil, errors.SeckillActivityNotFound
	}

	sku := &seckill_sku.SeckillSkus{
		Id:            seckill_sku.NewSeckillSkuId(),
		ActivityId:    in.ActivityId,
		Title:         in.Title,
		Subtitle:      in.Subtitle,
		Pic:           in.Pic,
		OriginalPrice: in.OriginalPrice,
		SeckillPrice:  in.SeckillPrice,
		Stock:         int(in.Stock),
		Sold:          0,
		LockStock:     0,
		Status:        1,
		Sort:          int(in.Sort),
	}

	// 指定商城 SKU 时，从 mall-rpc 预加载商品信息做快照；未传入的字段以商城数据兜底
	if in.MallSkuId != "" {
		resp, err := l.svcCtx.MallProductClient.GetSku(l.ctx, &productservice.GetSkuReq{Id: in.MallSkuId})
		if err != nil {
			l.Logger.Errorf("failed to load mall sku %s: %v", in.MallSkuId, err)
			return nil, errors.Internal
		}
		mallSku := resp.GetSku()
		if mallSku == nil || mallSku.Id == "" {
			l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
			return nil, errors.MallSkuNotFound
		}
		// 商城 SKU 已下架则不允许接入秒杀
		if mallSku.Status != 1 {
			l.Logger.Errorf("return error: %v", errors.MallSkuNotFound)
			return nil, errors.MallSkuNotFound
		}

		sku.MallSkuId = mallSku.Id
		sku.MallProductId = mallSku.ProductId
		if sku.Title == "" {
			sku.Title = mallSku.Name
		}
		if sku.OriginalPrice <= 0 {
			sku.OriginalPrice = mallSku.Price
		}
		if sku.Stock <= 0 {
			sku.Stock = int(mallSku.Stock)
		}
	}

	// 基础校验
	if sku.Stock < 0 {
		l.Logger.Errorf("return error: %v", errors.Internal)
		return nil, errors.Internal
	}
	if sku.SeckillPrice <= 0 || (sku.OriginalPrice > 0 && sku.SeckillPrice > sku.OriginalPrice) {
		l.Logger.Errorf("return error: %v", errors.Invalid)
		return nil, errors.Invalid
	}

	if err := l.svcCtx.SkuStore.InsertOne(l.ctx, sku); err != nil {
		l.Logger.Errorf("failed to create sku: %v", err)
		return nil, errors.Database
	}

	return &pb.CreateSkuResp{Id: sku.Id}, nil
}
