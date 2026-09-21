package productservicelogic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/internal/svc"
	"budgetmatch-sim/services/rpc/mall/model/product_index"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type candidateReaderFunc func(context.Context, []string) ([]product_index.CandidateFacts, error)

func (f candidateReaderFunc) FindActiveCandidates(ctx context.Context, ids []string) ([]product_index.CandidateFacts, error) {
	return f(ctx, ids)
}

func TestCandidateCheckOneBoundedQueryAndExplicitAbsence(t *testing.T) {
	calls := 0
	var queryCtx context.Context
	store := candidateReaderFunc(func(ctx context.Context, ids []string) ([]product_index.CandidateFacts, error) {
		calls++
		queryCtx = ctx
		require.Equal(t, []string{"a", "missing", "empty-stock"}, ids)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), candidatecontract.MaxDuration)
		return []product_index.CandidateFacts{{SkuId: "empty-stock", ProductId: "p", Price: 100},
			{SkuId: "a", ProductId: "p", Price: 150, Stock: 5, Sold: 20}}, nil
	})
	start := time.Now().UnixMilli()
	resp, err := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: store}).
		CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a", "missing", "empty-stock"}})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.GreaterOrEqual(t, resp.CheckedAtUnixMs, start)
	require.Len(t, resp.Results, 3)
	require.Equal(t, pb.CandidateState_CANDIDATE_STATE_ACTIVE, resp.Results[0].State)
	require.EqualValues(t, 150, resp.Results[0].Facts.Price)
	require.Equal(t, pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE, resp.Results[1].State)
	require.Nil(t, resp.Results[1].Facts)
	require.Equal(t, pb.CandidateState_CANDIDATE_STATE_ACTIVE, resp.Results[2].State)
	require.Zero(t, resp.Results[2].Facts.Stock)
	require.ErrorIs(t, queryCtx.Err(), context.Canceled)
}

func TestCandidateCheckFailureIsNotAbsence(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []product_index.CandidateFacts
		err  error
		code codes.Code
	}{
		{"database", nil, errors.New("secret-dsn"), codes.Unavailable},
		{"unexpected row", []product_index.CandidateFacts{{SkuId: "other", ProductId: "p"}}, nil, codes.Unavailable},
		{"missing parent", []product_index.CandidateFacts{{SkuId: "a"}}, nil, codes.Unavailable},
		{"duplicate", []product_index.CandidateFacts{{SkuId: "a", ProductId: "p"}, {SkuId: "a", ProductId: "p"}}, nil, codes.Unavailable},
		{"oversized", []product_index.CandidateFacts{{SkuId: "a", ProductId: "p", ProductName: strings.Repeat("x", candidatecontract.MaxResponseBytes)}}, nil, codes.ResourceExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := candidateReaderFunc(func(context.Context, []string) ([]product_index.CandidateFacts, error) { return tc.rows, tc.err })
			resp, err := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: store}).
				CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}})
			require.Nil(t, resp)
			require.Equal(t, tc.code, status.Code(err))
			require.NotContains(t, err.Error(), "secret-dsn")
		})
	}
}

func TestCandidateCheckRejectsInvalidAndCanceledBeforeStore(t *testing.T) {
	store := candidateReaderFunc(func(context.Context, []string) ([]product_index.CandidateFacts, error) {
		t.Fatal("store must not be called")
		return nil, nil
	})
	logic := NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{CandidateStore: store})
	for _, req := range []*pb.CheckProductCandidatesReq{nil, {}, {SkuIds: []string{"a", "a"}}} {
		_, err := logic.CheckProductCandidates(req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewCheckProductCandidatesLogic(ctx, &svc.ServiceContext{CandidateStore: store}).
		CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}})
	require.Equal(t, codes.Canceled, status.Code(err))
	_, err = NewCheckProductCandidatesLogic(context.Background(), &svc.ServiceContext{}).
		CheckProductCandidates(&pb.CheckProductCandidatesReq{SkuIds: []string{"a"}})
	require.Equal(t, codes.Unavailable, status.Code(err))
}
