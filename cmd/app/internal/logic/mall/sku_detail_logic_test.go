package mall

import (
	"context"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	"budgetmatch-sim/infra/errors"
	"budgetmatch-sim/services/rpc/mall/client/productservice"
	"budgetmatch-sim/services/rpc/mall/pb"
	"google.golang.org/grpc"
)

type skuDetailClient struct {
	productservice.ProductService
	response    *pb.GetSkuResp
	err         error
	requestedID string
}

func (c *skuDetailClient) GetSku(_ context.Context, req *pb.GetSkuReq, _ ...grpc.CallOption) (*pb.GetSkuResp, error) {
	c.requestedID = req.Id
	return c.response, c.err
}

func TestSkuDetailMapsActualSKU(t *testing.T) {
	client := &skuDetailClient{response: &pb.GetSkuResp{Sku: &pb.Sku{
		Id: "sku-1", ProductId: "product-1", Name: "绿色", Specs: `{"颜色":"绿"}`,
		Price: 19900, Stock: 3, Sold: 9, Status: 1, AgentComment: "便携",
		CreatedAt: "created", UpdatedAt: "updated",
	}}}
	logic := NewSkuDetailLogic(context.Background(), &svc.ServiceContext{MallProductClient: client})
	result, err := logic.SkuDetail(&types.MallSkuDetailReq{Id: "sku-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := types.MallSkuItem{Id: "sku-1", ProductId: "product-1", Name: "绿色", Specs: `{"颜色":"绿"}`, Price: 19900, Stock: 3, Sold: 9, Status: 1, AgentComment: "便携", CreatedAt: "created", UpdatedAt: "updated"}
	if result.Sku != want || client.requestedID != "sku-1" {
		t.Fatalf("unexpected SKU: %+v", result.Sku)
	}
}

func TestSkuDetailMissingAndRPCFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *pb.GetSkuResp
		err      error
		want     error
	}{
		{"nil response", nil, nil, errors.MallSkuNotFound},
		{"nil sku", &pb.GetSkuResp{}, nil, errors.MallSkuNotFound},
		{"rpc failure", nil, errors.Database, errors.Database},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logic := NewSkuDetailLogic(context.Background(), &svc.ServiceContext{MallProductClient: &skuDetailClient{response: tc.response, err: tc.err}})
			result, err := logic.SkuDetail(&types.MallSkuDetailReq{Id: "missing"})
			if result != nil || err != tc.want {
				t.Fatalf("got %v, %v; want nil, %v", result, err, tc.want)
			}
		})
	}
}
