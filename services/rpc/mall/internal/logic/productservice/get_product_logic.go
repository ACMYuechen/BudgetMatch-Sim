package productservicelogic

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/products"
	"budgetmatch-sim/services/rpc/mall/pb"
)

type GetProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductLogic {
	return &GetProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetProductLogic) GetProduct(in *pb.GetProductReq) (*pb.GetProductResp, error) {
	// try cache
	key := productCacheKey(in.Id)
	cached, err := l.svcCtx.Redis.Get(l.ctx, key).Result()
	if err == nil {
		var p products.Products
		if err := json.Unmarshal([]byte(cached), &p); err == nil {
			return &pb.GetProductResp{Product: productToPb(&p)}, nil
		}
	} else if err != redis.Nil {
		l.Logger.Errorf("failed to get product from cache: %v", err)
	}

	product, err := l.svcCtx.ProductStore.FindOne(l.ctx, in.Id)
	if err != nil {
		l.Logger.Errorf("failed to find product: %v", err)
		return nil, errors.Database
	}
	if product == nil {
		// cache null object
		_ = l.svcCtx.Redis.Set(l.ctx, key, "null", time.Minute).Err()
		return nil, errors.MallProductNotFound
	}

	// cache product
	if data, err := json.Marshal(product); err == nil {
		_ = l.svcCtx.Redis.Set(l.ctx, key, data, 10*time.Minute).Err()
	}

	return &pb.GetProductResp{Product: productToPb(product)}, nil
}
