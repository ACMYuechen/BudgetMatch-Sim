package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type candidateClientFunc func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error)

func (f candidateClientFunc) CheckProductCandidates(ctx context.Context, req *pb.CheckProductCandidatesReq, _ ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	return f(ctx, req)
}

func staleCandidate(id string) ProductCandidate {
	return ProductCandidate{Id: id, Name: "old", PriceCents: 100, Stock: 1, Category: "untrusted-old-label", Source: "mall+rag",
		Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallVector, ProductID: "p", Relevance: .8, HasRelevance: true, SnapshotAtUnixMs: 10}}
}
func activeCheck(id string) *pb.CandidateCheck {
	return &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_ACTIVE,
		Facts: &pb.CandidateFacts{SkuId: id, ProductId: "p", ProductName: "new", SkuName: "sku", Price: 200, Stock: 3, Sold: 5}}
}
func checkResponse(ids ...string) *pb.CheckProductCandidatesResp {
	r := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli()}
	for _, id := range ids {
		r.Results = append(r.Results, activeCheck(id))
	}
	return r
}

func TestMallVerifierUpdatesFactsAndDropsConfirmedUnavailable(t *testing.T) {
	calls := 0
	var rpcCtx context.Context
	client := candidateClientFunc(func(ctx context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		calls++
		rpcCtx = ctx
		require.Equal(t, []string{"a", "b", "c", "d"}, req.SkuIds)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), candidatecontract.MaxDuration)
		r := checkResponse("d", "c", "b", "a")
		r.Results[1].Facts.Stock = 0
		r.Results[2] = &pb.CandidateCheck{SkuId: "b", State: pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE}
		return r, nil
	})
	input := []ProductCandidate{staleCandidate("a"), staleCandidate("b"), staleCandidate("c"), staleCandidate("d")}
	batch, err := NewMallCandidateVerifier(client).Verify(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Len(t, batch.Candidates, 2)
	require.Equal(t, "a", batch.Candidates[0].Id)
	require.Equal(t, "d", batch.Candidates[1].Id)
	for _, candidate := range batch.Candidates {
		require.Equal(t, "new sku", candidate.Name)
		require.EqualValues(t, 200, candidate.PriceCents)
		require.EqualValues(t, 3, candidate.Stock)
		require.Empty(t, candidate.Category)
		require.Equal(t, agentcore.VerificationChecked, candidate.Evidence.State)
		require.Equal(t, batch.CheckedAtUnixMs, candidate.Evidence.VerifiedAtUnixMs)
		require.Equal(t, .8, candidate.Evidence.Relevance)
		require.EqualValues(t, 10, candidate.Evidence.SnapshotAtUnixMs)
	}
	require.EqualValues(t, 100, input[0].PriceCents, "do not mutate stale input")
	require.ErrorIs(t, rpcCtx.Err(), context.Canceled)
}

func TestMallVerifierRejectsIncompleteOrMalformedResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp
	}{
		{"nil", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp { return nil }},
		{"missing", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results = r.Results[:1]
			return r
		}},
		{"nil entry", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp { r.Results[0] = nil; return r }},
		{"duplicate", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[1] = r.Results[0]
			return r
		}},
		{"unknown SKU", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[1] = activeCheck("x")
			return r
		}},
		{"missing facts", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].Facts = nil
			return r
		}},
		{"wrong SKU", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].Facts.SkuId = "x"
			return r
		}},
		{"wrong parent", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].Facts.ProductId = "x"
			return r
		}},
		{"unknown state", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].State = 0
			return r
		}},
		{"contradictory absence", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].State = pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE
			return r
		}},
		{"missing time", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp { r.CheckedAtUnixMs = 0; return r }},
		{"stale time", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.CheckedAtUnixMs -= 60000
			return r
		}},
		{"future time", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.CheckedAtUnixMs += 60000
			return r
		}},
		{"oversized", func(r *pb.CheckProductCandidatesResp) *pb.CheckProductCandidatesResp {
			r.Results[0].Facts.ProductName = strings.Repeat("x", candidatecontract.MaxResponseBytes)
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := candidateClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				return tc.change(checkResponse("a", "b")), nil
			})
			batch, err := NewMallCandidateVerifier(client).Verify(context.Background(), []ProductCandidate{staleCandidate("a"), staleCandidate("b")})
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Empty(t, batch.Candidates)
		})
	}
}

func TestMallVerifierDependencyErrorsAndCancellation(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.Unimplemented, codes.Internal, codes.DeadlineExceeded, codes.Unauthenticated, codes.PermissionDenied, codes.Canceled} {
		t.Run(code.String(), func(t *testing.T) {
			client := candidateClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				return nil, status.Error(code, "secret-dsn")
			})
			_, err := NewMallCandidateVerifier(client).Verify(context.Background(), []ProductCandidate{staleCandidate("a")})
			want := codes.Unavailable
			if code == codes.Unauthenticated || code == codes.PermissionDenied || code == codes.Canceled {
				want = code
			}
			require.Equal(t, want, status.Code(err))
			require.NotContains(t, err.Error(), "secret-dsn")
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := candidateClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		cancel()
		return checkResponse("a"), nil
	})
	_, err := NewMallCandidateVerifier(client).Verify(ctx, []ProductCandidate{staleCandidate("a")})
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewMallCandidateVerifier(nil).Verify(ctx, nil)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestMallVerifierDoesNotTrustSourceString(t *testing.T) {
	for _, source := range []agentcore.RetrievalSource{agentcore.RetrievalUnknown, agentcore.RetrievalDemo} {
		candidate := staleCandidate("a")
		candidate.Source = "mall"
		candidate.Evidence.Source = source
		client := candidateClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
			t.Fatal("invalid evidence reached RPC")
			return nil, nil
		})
		_, err := NewMallCandidateVerifier(client).Verify(context.Background(), []ProductCandidate{candidate})
		require.Equal(t, codes.Unavailable, status.Code(err))
	}
}
