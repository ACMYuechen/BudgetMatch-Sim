package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rankedMallClient struct {
	products func(context.Context, *pb.ListProductsReq) (*pb.ListProductsResp, error)
	skus     func(context.Context, *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error)
}

func (c rankedMallClient) ListProducts(ctx context.Context, req *pb.ListProductsReq, _ ...grpc.CallOption) (*pb.ListProductsResp, error) {
	return c.products(ctx, req)
}
func (c rankedMallClient) ListSkusByProduct(ctx context.Context, req *pb.ListSkusByProductReq, _ ...grpc.CallOption) (*pb.ListSkusByProductResp, error) {
	return c.skus(ctx, req)
}

func TestRankedMallWindowRetainsInvalidFactsAndUsesStablePageSize(t *testing.T) {
	var pages, sizes []int32
	provider := NewMallProductProvider(rankedMallClient{
		products: func(_ context.Context, req *pb.ListProductsReq) (*pb.ListProductsResp, error) {
			require.EqualValues(t, 1, req.Status)
			return &pb.ListProductsResp{List: []*pb.Product{{Id: "p", Name: "keyboard", Status: 1}}, Total: 1}, nil
		},
		skus: func(_ context.Context, req *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error) {
			pages = append(pages, req.Page)
			sizes = append(sizes, req.PageSize)
			resp := &pb.ListSkusByProductResp{Total: 70}
			for i := int32(0); i < req.PageSize; i++ {
				resp.List = append(resp.List, &pb.Sku{Id: fmt.Sprintf("s-%d", (req.Page-1)*req.PageSize+i), ProductId: "p", Price: 9999, Stock: 0, Status: 1})
			}
			return resp, nil
		},
	})
	got, err := provider.SearchRankedProducts(context.Background(), hybridRequest(), 40)
	require.NoError(t, err)
	require.Len(t, got, 40)
	require.Equal(t, []int32{1, 2}, pages)
	require.Equal(t, []int32{32, 32}, sizes)
	require.Equal(t, "s-39", got[39].Id)
	require.Zero(t, got[0].Stock)
	require.EqualValues(t, 9999, got[0].PriceCents)
}

func TestRankedMallBoundsRPCWorkEvenWhenEveryProductHasNoSKUs(t *testing.T) {
	calls := 0
	provider := NewMallProductProvider(rankedMallClient{
		products: func(_ context.Context, req *pb.ListProductsReq) (*pb.ListProductsResp, error) {
			calls++
			resp := &pb.ListProductsResp{Total: 10000}
			for i := 0; i < int(req.PageSize); i++ {
				resp.List = append(resp.List, &pb.Product{Id: fmt.Sprintf("p-%s-%d-%d", req.Keyword, req.Page, i), Status: 1})
			}
			return resp, nil
		},
		skus: func(context.Context, *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error) {
			calls++
			return &pb.ListSkusByProductResp{}, nil
		},
	})
	got, err := provider.SearchRankedProducts(context.Background(), hybridRequest(), 64)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, maxRankedMallCalls, calls)
}

func TestRankedMallLimitsTermsAndRejectsMalformedResponses(t *testing.T) {
	var terms []string
	provider := NewMallProductProvider(rankedMallClient{products: func(_ context.Context, req *pb.ListProductsReq) (*pb.ListProductsResp, error) {
		terms = append(terms, req.Keyword)
		return &pb.ListProductsResp{}, nil
	}})
	req := hybridRequest()
	req.Keywords = []string{"a", "b", "c", "d", "e", "f"}
	_, err := provider.SearchRankedProducts(context.Background(), req, 4)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c", "d"}, terms)
	for _, response := range []*pb.ListProductsResp{
		nil, {List: []*pb.Product{nil}}, {List: []*pb.Product{{Id: "bad id ", Status: 1}}},
		{List: []*pb.Product{{Id: "p", Status: 1, Content: strings.Repeat("x", maxRankedResponseBytes)}}},
	} {
		provider := NewMallProductProvider(rankedMallClient{products: func(context.Context, *pb.ListProductsReq) (*pb.ListProductsResp, error) { return response, nil }})
		_, err := provider.SearchRankedProducts(context.Background(), hybridRequest(), 4)
		require.Equal(t, codes.Unavailable, status.Code(err))
	}
	provider = NewMallProductProvider(nil)
	for _, k := range []int{-1, 0, 65} {
		_, err := provider.SearchRankedProducts(context.Background(), hybridRequest(), k)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = provider.SearchRankedProducts(ctx, hybridRequest(), 4)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRankedMallRejectsMalformedSKUsAndRetainsOffShelfInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp *pb.ListSkusByProductResp
		code codes.Code
	}{
		{"nil response", nil, codes.Unavailable},
		{"nil SKU", &pb.ListSkusByProductResp{List: []*pb.Sku{nil}}, codes.Unavailable},
		{"parent mismatch", &pb.ListSkusByProductResp{List: []*pb.Sku{{Id: "s", ProductId: "other", Status: 1}}}, codes.Unavailable},
		{"oversized", &pb.ListSkusByProductResp{List: []*pb.Sku{{Id: "s", ProductId: "p", Name: strings.Repeat("x", maxRankedResponseBytes), Status: 1}}}, codes.Unavailable},
		{"too many rows", &pb.ListSkusByProductResp{List: make([]*pb.Sku, 5)}, codes.Unavailable},
		{"off shelf", &pb.ListSkusByProductResp{List: []*pb.Sku{{Id: "s", ProductId: "p", Price: 100, Stock: 2, Status: 0}}, Total: 1}, codes.OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewMallProductProvider(rankedMallClient{
				products: func(context.Context, *pb.ListProductsReq) (*pb.ListProductsResp, error) {
					return &pb.ListProductsResp{List: []*pb.Product{{Id: "p", Status: 1}}, Total: 1}, nil
				},
				skus: func(context.Context, *pb.ListSkusByProductReq) (*pb.ListSkusByProductResp, error) {
					return tc.resp, nil
				},
			})
			got, err := provider.SearchRankedProducts(context.Background(), hybridRequest(), 4)
			require.Equal(t, tc.code, status.Code(err))
			if tc.code == codes.OK {
				require.Len(t, got, 1)
				require.Zero(t, got[0].Stock)
			} else {
				require.Empty(t, got)
			}
		})
	}
}
