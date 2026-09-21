package productindexservicelogic

import (
	"context"
	"errors"
	"strings"
	"testing"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/infra/serviceauth"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
)

type readerFunc func(context.Context, string, int) ([]product_index.Entry, error)

func (f readerFunc) ListPage(ctx context.Context, cursor string, limit int) ([]product_index.Entry, error) {
	return f(ctx, cursor, limit)
}

func indexContext() context.Context {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyServiceName, serviceauth.ServiceAgent)
	return context.WithValue(ctx, interceptor.ContextKeyServicePurpose, serviceauth.PurposeProductIndexRead)
}

func TestIndexLogicRequiresCallerAndPurpose(t *testing.T) {
	for _, ctx := range []context.Context{
		context.Background(),
		context.WithValue(context.Background(), interceptor.ContextKeyUserId, "user-1"),
		context.WithValue(context.Background(), interceptor.ContextKeyServiceName, serviceauth.ServiceAgent),
		context.WithValue(indexContext(), interceptor.ContextKeyServiceName, serviceauth.ServicePayment),
		context.WithValue(indexContext(), interceptor.ContextKeyServicePurpose, "order:write"),
	} {
		resp, err := NewListProductIndexLogic(ctx, &svc.ServiceContext{}).ListProductIndex(&pb.ListProductIndexReq{})
		require.ErrorIs(t, err, apperrors.Unauthorized)
		require.Nil(t, resp)
	}
}

func TestIndexLogicValidatesBoundsBeforeRead(t *testing.T) {
	for _, req := range []*pb.ListProductIndexReq{nil, {PageSize: -1}, {PageSize: 201},
		{Cursor: strings.Repeat("x", 129)}, {Cursor: " invalid"}, {Cursor: "invalid\x00"}} {
		resp, err := NewListProductIndexLogic(indexContext(), &svc.ServiceContext{}).ListProductIndex(req)
		require.ErrorIs(t, err, apperrors.Invalid)
		require.Nil(t, resp)
	}
}

func TestIndexLogicKeysetPages(t *testing.T) {
	rows := []product_index.Entry{
		{SkuId: "s1", ProductId: "p1", ProductName: "键盘", ProductContent: "办公", Provider: "K", SkuName: "红轴", Specs: `{}`, Price: 29900, Stock: 10, Sold: 5},
		{SkuId: "s2", ProductId: "p1"},
		{SkuId: "s3", ProductId: "p2"},
	}
	for _, tc := range []struct {
		name, cursor string
		size, limit  int
		start, count int
		next         string
		complete     bool
	}{
		{"first", "", 2, 3, 0, 2, "s2", false},
		{"last", "s2", 2, 3, 2, 1, "", true},
		{"empty", "s3", 2, 3, 3, 0, "", true},
		{"default", "", 0, 101, 0, 3, "", true},
		{"maximum", "", 200, 201, 0, 3, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := readerFunc(func(ctx context.Context, cursor string, limit int) ([]product_index.Entry, error) {
				require.Equal(t, tc.cursor, cursor)
				require.Equal(t, tc.limit, limit)
				require.Equal(t, serviceauth.ServiceAgent, ctx.Value(interceptor.ContextKeyServiceName))
				return rows[tc.start:], nil
			})
			resp, err := NewListProductIndexLogic(indexContext(), &svc.ServiceContext{ProductIndexStore: store}).
				ListProductIndex(&pb.ListProductIndexReq{Cursor: tc.cursor, PageSize: int32(tc.size)})
			require.NoError(t, err)
			require.Len(t, resp.List, tc.count)
			require.Equal(t, tc.next, resp.NextCursor)
			require.Equal(t, tc.complete, resp.Complete)
			if tc.start == 0 {
				first := resp.List[0]
				require.Equal(t, "键盘", first.ProductName)
				require.Equal(t, "办公", first.ProductContent)
				require.Equal(t, "K", first.Provider)
				require.Equal(t, "红轴", first.SkuName)
				require.Equal(t, `{}`, first.Specs)
				require.EqualValues(t, 29900, first.Price)
				require.EqualValues(t, 10, first.Stock)
				require.EqualValues(t, 5, first.Sold)
			}
		})
	}
}

func TestIndexLogicReadErrorCannotBecomeCompleteEmptyPage(t *testing.T) {
	store := readerFunc(func(context.Context, string, int) ([]product_index.Entry, error) {
		return nil, errors.New("private-database-details")
	})
	resp, err := NewListProductIndexLogic(indexContext(), &svc.ServiceContext{ProductIndexStore: store}).ListProductIndex(&pb.ListProductIndexReq{})
	require.ErrorIs(t, err, apperrors.Database)
	require.NotContains(t, err.Error(), "private-database-details")
	require.Nil(t, resp)
	resp, err = NewListProductIndexLogic(indexContext(), &svc.ServiceContext{}).ListProductIndex(&pb.ListProductIndexReq{})
	require.ErrorIs(t, err, apperrors.Internal)
	require.Nil(t, resp)
}
