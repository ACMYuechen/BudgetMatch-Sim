package skuservicelogic

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
	"budgetmatch-sim/services/rpc/seckill/internal/svc"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_activity"
	"budgetmatch-sim/services/rpc/seckill/model/seckill_sku"
	"budgetmatch-sim/services/rpc/seckill/pb"
)

// 以下三个 fake 通过内嵌接口满足完整方法集，只覆盖被测逻辑实际调用的方法。

type fakeActivityStore struct {
	seckill_activity.SeckillActivitiesModel
}

func (fakeActivityStore) FindOne(ctx context.Context, id string) (*seckill_activity.SeckillActivities, error) {
	return &seckill_activity.SeckillActivities{}, nil // 活动存在即可
}

type fakeSkuStore struct {
	seckill_sku.SeckillSkusModel
	inserted *seckill_sku.SeckillSkus
}

func (f *fakeSkuStore) InsertOne(ctx context.Context, data *seckill_sku.SeckillSkus) error {
	f.inserted = data
	return nil
}

type fakeMallClient struct {
	productservice.ProductService
	resp *productservice.GetSkuResp
	err  error
}

func (f fakeMallClient) GetSku(ctx context.Context, in *productservice.GetSkuReq, opts ...grpc.CallOption) (*productservice.GetSkuResp, error) {
	return f.resp, f.err
}

func newLogic(skuStore *fakeSkuStore, mall productservice.ProductService) *CreateSkuLogic {
	sc := &svc.ServiceContext{
		ActivityStore:     fakeActivityStore{},
		SkuStore:          skuStore,
		MallProductClient: mall,
	}
	return NewCreateSkuLogic(context.Background(), sc)
}

func mallResp(sku *productservice.Sku) *productservice.GetSkuResp {
	return &productservice.GetSkuResp{Sku: sku}
}

func TestCreateSku_SnapshotFromMall(t *testing.T) {
	store := &fakeSkuStore{}
	mall := fakeMallClient{resp: mallResp(&productservice.Sku{
		Id: "mall-sku-1", ProductId: "mall-prod-1", Name: "iPhone", Price: 599900, Stock: 50, Status: 1,
	})}

	// 只传 mall_sku_id + 秒杀价，其余靠商城快照兜底
	_, err := newLogic(store, mall).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", MallSkuId: "mall-sku-1", SeckillPrice: 499900,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.inserted
	if got == nil {
		t.Fatal("sku was not inserted")
	}
	if got.Title != "iPhone" || got.OriginalPrice != 599900 || got.Stock != 50 {
		t.Fatalf("snapshot not applied: title=%q original=%d stock=%d", got.Title, got.OriginalPrice, got.Stock)
	}
	if got.MallSkuId != "mall-sku-1" || got.MallProductId != "mall-prod-1" {
		t.Fatalf("linkage not stored: mallSku=%q mallProduct=%q", got.MallSkuId, got.MallProductId)
	}
}

func TestCreateSku_RequestOverridesSnapshot(t *testing.T) {
	store := &fakeSkuStore{}
	mall := fakeMallClient{resp: mallResp(&productservice.Sku{
		Id: "mall-sku-1", ProductId: "mall-prod-1", Name: "iPhone", Price: 599900, Stock: 50, Status: 1,
	})}

	_, err := newLogic(store, mall).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", MallSkuId: "mall-sku-1",
		Title: "custom", OriginalPrice: 600000, Stock: 10, SeckillPrice: 499900,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := store.inserted
	if got.Title != "custom" || got.OriginalPrice != 600000 || got.Stock != 10 {
		t.Fatalf("request values should win: title=%q original=%d stock=%d", got.Title, got.OriginalPrice, got.Stock)
	}
	if got.MallSkuId != "mall-sku-1" {
		t.Fatalf("linkage should still be stored, got %q", got.MallSkuId)
	}
}

func TestCreateSku_MallSkuOffline(t *testing.T) {
	store := &fakeSkuStore{}
	mall := fakeMallClient{resp: mallResp(&productservice.Sku{Id: "mall-sku-1", Status: 0})} // 下架

	_, err := newLogic(store, mall).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", MallSkuId: "mall-sku-1", SeckillPrice: 100,
	})
	if err != errors.MallSkuNotFound {
		t.Fatalf("expected MallSkuNotFound, got %v", err)
	}
	if store.inserted != nil {
		t.Fatal("should not insert when mall sku is offline")
	}
}

func TestCreateSku_MallSkuMissing(t *testing.T) {
	store := &fakeSkuStore{}
	mall := fakeMallClient{resp: mallResp(nil)} // 商城无此 SKU

	_, err := newLogic(store, mall).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", MallSkuId: "ghost", SeckillPrice: 100,
	})
	if err != errors.MallSkuNotFound {
		t.Fatalf("expected MallSkuNotFound, got %v", err)
	}
}

func TestCreateSku_ManualNoMallLink(t *testing.T) {
	store := &fakeSkuStore{}
	// 不传 mall_sku_id：MallProductClient 不应被调用，传 nil 以确保未被使用
	_, err := newLogic(store, nil).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", Title: "manual", OriginalPrice: 1000, Stock: 5, SeckillPrice: 800,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.inserted.MallSkuId != "" {
		t.Fatalf("manual sku should have empty MallSkuId, got %q", store.inserted.MallSkuId)
	}
}

func TestCreateSku_SeckillPriceAboveOriginalRejected(t *testing.T) {
	store := &fakeSkuStore{}
	mall := fakeMallClient{resp: mallResp(&productservice.Sku{Id: "mall-sku-1", Name: "x", Price: 100, Stock: 5, Status: 1})}

	// 原价取商城的 100，秒杀价 200 > 原价 → 应拒绝
	_, err := newLogic(store, mall).CreateSku(&pb.CreateSkuReq{
		ActivityId: "act1", MallSkuId: "mall-sku-1", SeckillPrice: 200,
	})
	if err != errors.Invalid {
		t.Fatalf("expected Invalid, got %v", err)
	}
}
