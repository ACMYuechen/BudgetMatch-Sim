package demandexec

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rankedSourceFunc func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error)

func (f rankedSourceFunc) SearchRankedProducts(ctx context.Context, req tools.SearchProductsReq, limit int) ([]agent.ProductCandidate, error) {
	return f(ctx, req, limit)
}

type checkClientFunc func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error)

func (f checkClientFunc) CheckProductCandidates(ctx context.Context, req *pb.CheckProductCandidatesReq, _ ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	return f(ctx, req)
}

func mallRaw() []agent.ProductCandidate {
	var out []agent.ProductCandidate
	for _, id := range []string{"a", "b", "c"} {
		out = append(out, agent.ProductCandidate{Id: id, Name: "stale", Category: "invented", PriceCents: 1, Stock: 1,
			Evidence: agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, ProductID: "p-" + id}})
	}
	return out
}

func mallFacts(req *pb.CheckProductCandidatesReq) *pb.CheckProductCandidatesResp {
	r := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli(), DemandCategoryContract: candidatecontract.DemandCategoryContract}
	for _, id := range req.SkuIds {
		code, price := "keyboard", int64(100)
		if id == "b" {
			price = 130
		}
		if id == "c" {
			code, price = "mouse", 50
		}
		r.Results = append(r.Results, &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_ACTIVE,
			Facts: &pb.CandidateFacts{SkuId: id, ProductId: "p-" + id, ProductName: "checked " + id, Price: price, Stock: 2,
				DemandCategory: &pb.DemandCategoryFact{Code: code, TaxonomyVersion: candidatecontract.DemandTaxonomyVersion, Revision: 3}}})
	}
	return r
}

func mallIntent() agent.Intent {
	return agent.Intent{BudgetCents: 220, MaxItems: 2, Keywords: []string{"desk"}, Preferences: []string{"quiet"},
		Demand: &agent.DemandState{SchemaVersion: 1, Required: []string{"keyboard", "mouse"}, Excluded: []string{"phone"}}}
}

func TestMallExecutionRechecksAndReselectsWithinSameBoundedWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*pb.CheckProductCandidatesResp)
		wantStatus string
		wantTotal  int64
	}{
		{"unchanged", func(*pb.CheckProductCandidatesResp) {}, "complete", 150},
		{"price increased", func(r *pb.CheckProductCandidatesResp) { r.Results[0].Facts.Price = 300 }, "complete", 180},
		{"price decreased", func(r *pb.CheckProductCandidatesResp) { r.Results[1].Facts.Price = 80 }, "complete", 130},
		{"off shelf", func(r *pb.CheckProductCandidatesResp) {
			r.Results[0].State = pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE
			r.Results[0].Facts = nil
		}, "complete", 180},
		{"classification changed", func(r *pb.CheckProductCandidatesResp) {
			r.Results[0].Facts.DemandCategory.Code = "phone"
			r.Results[0].Facts.DemandCategory.Revision++
		}, "complete", 180},
		{"classification withdrawn", func(r *pb.CheckProductCandidatesResp) {
			r.Results[0].Facts.DemandCategory.Code = "unknown"
			r.Results[0].Facts.DemandCategory.Revision = 0
		}, "complete", 180},
		{"no affordable alternative", func(r *pb.CheckProductCandidatesResp) { r.Results[0].Facts.Price = 300; r.Results[1].Facts.Stock = 0 }, "no_feasible_bundle", 0},
		{"required category lost", func(r *pb.CheckProductCandidatesResp) {
			r.Results[2].Facts.DemandCategory.Code = "unknown"
			r.Results[2].Facts.DemandCategory.Revision++
		}, "no_feasible_bundle", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			searches, checks := 0, 0
			provider := rankedSourceFunc(func(ctx context.Context, req tools.SearchProductsReq, limit int) ([]agent.ProductCandidate, error) {
				searches++
				require.Equal(t, 32, limit)
				require.Equal(t, []string{"键盘", "鼠标", "desk"}, req.Keywords)
				require.EqualValues(t, 220, req.BudgetCents)
				_, ok := ctx.Deadline()
				require.True(t, ok)
				return mallRaw(), nil
			})
			client := checkClientFunc(func(ctx context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				checks++
				require.True(t, req.IncludeDemandCategory)
				require.Equal(t, []string{"a", "b", "c"}, req.SkuIds)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), candidatecontract.MaxDuration)
				r := mallFacts(req)
				if checks == 2 {
					tc.change(r)
				}
				return r, nil
			})
			e, err := NewMall(provider, client)
			require.NoError(t, err)
			result, err := e.Run(context.Background(), mallIntent())
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, result.Status)
			require.Equal(t, tc.wantTotal, result.TotalPriceCents)
			require.Equal(t, 1, searches)
			require.Equal(t, 2, checks)
			require.Equal(t, "mall_checked_snapshot_only", result.Execution.Scope)
			require.Equal(t, "mall_demand_mapping_v1", result.Execution.MappingVersion)
			require.Len(t, result.Execution.MappingSHA256, 64)
			require.Greater(t, result.Execution.SnapshotCheckedAtUnixMs, int64(0))
			require.True(t, result.Execution.SearchLimited)
			require.Equal(t, []string{"quiet"}, result.Execution.UnscoredPreferences)
			require.Equal(t, "demand.mall_recheck", result.ToolsUsed[1].Name)
			require.Contains(t, result.Summary, "非库存预占")
			require.NotContains(t, result.Summary, "演示")
			if tc.wantTotal == 0 {
				require.Empty(t, result.Items)
			} else {
				require.Len(t, result.Items, 2)
				for _, item := range result.Items {
					require.Equal(t, "mall", item.Source)
					require.NotEqual(t, "invented", item.Category)
					require.Contains(t, item.Name, "checked")
				}
			}
		})
	}
}

func TestMallExecutionFailsClosedAtEitherCheck(t *testing.T) {
	for _, attempt := range []int{1, 2} {
		for _, kind := range []string{"old server", "missing", "wrong parent", "denied", "canceled", "revision rollback", "category without revision"} {
			t.Run(kind+string(rune('0'+attempt)), func(t *testing.T) {
				checks := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
					return mallRaw(), nil
				})
				client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
					checks++
					r := mallFacts(req)
					if checks == attempt {
						switch kind {
						case "old server":
							r.DemandCategoryContract = ""
						case "missing":
							r.Results = r.Results[:2]
						case "wrong parent":
							r.Results[0].Facts.ProductId = "other"
						case "denied":
							return nil, status.Error(codes.PermissionDenied, "private upstream")
						case "canceled":
							cancel()
						case "revision rollback":
							r.Results[0].Facts.DemandCategory.Revision = 2
						case "category without revision":
							r.Results[0].Facts.DemandCategory.Code = "phone"
						}
					}
					return r, nil
				})
				e, err := NewMall(provider, client)
				require.NoError(t, err)
				result, err := e.Run(ctx, mallIntent())
				// The initial snapshot has no older revision to compare against.
				if attempt == 1 && (kind == "revision rollback" || kind == "category without revision") {
					if kind == "revision rollback" {
						require.NoError(t, err)
						require.NotNil(t, result)
					} else {
						require.NoError(t, err)
						require.EqualValues(t, 180, result.TotalPriceCents)
					}
					return
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, attempt, checks)
				require.NotContains(t, err.Error(), "private upstream")
				if kind == "denied" {
					require.Equal(t, codes.PermissionDenied, status.Code(err))
				}
				if kind == "canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			})
		}
	}
}

func TestMallExecutionRejectsUnboundedOrForeignRetrievalBeforeCheck(t *testing.T) {
	for _, kind := range []string{"count", "bytes", "tags", "demo", "vector", "parent conflict", "bad ID", "error"} {
		t.Run(kind, func(t *testing.T) {
			provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
				r := mallRaw()
				switch kind {
				case "count":
					return make([]agent.ProductCandidate, 33), nil
				case "bytes":
					r[0].Name = strings.Repeat("x", 1<<20)
				case "tags":
					r[0].Tags = make([]string, 33)
				case "demo":
					r[0].Evidence.Source = agent.RetrievalDemo
				case "vector":
					r[0].Evidence.Source = agent.RetrievalMallVector
				case "parent conflict":
					c := r[0]
					c.Evidence.ProductID = "other"
					r = append(r, c)
				case "bad ID":
					r[0].Id = " a "
				case "error":
					return nil, errors.New("search unavailable")
				}
				return r, nil
			})
			client := checkClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
				t.Fatal("invalid retrieval reached check")
				return nil, nil
			})
			e, err := NewMall(provider, client)
			require.NoError(t, err)
			result, err := e.Run(context.Background(), mallIntent())
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestMallExecutionCatalogIsRequestLocalAndEmptySearchDoesNotCertifyFacts(t *testing.T) {
	provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
		return mallRaw(), nil
	})
	client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		return mallFacts(req), nil
	})
	e, err := NewMall(provider, client)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.Run(context.Background(), mallIntent())
			if err != nil || r == nil || r.TotalPriceCents != 150 {
				t.Errorf("concurrent execution: result=%v error=%v", r, err)
			}
		}()
	}
	wg.Wait()
	empty := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) { return nil, nil })
	noCheck := checkClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		t.Fatal("empty search reached RPC")
		return nil, nil
	})
	e, err = NewMall(empty, noCheck)
	require.NoError(t, err)
	r, err := e.Run(context.Background(), mallIntent())
	require.NoError(t, err)
	require.Equal(t, "no_feasible_bundle", r.Status)
	require.Zero(t, r.Execution.SnapshotCheckedAtUnixMs)
	require.Len(t, r.ToolsUsed, 1)
	_, err = NewMall(nil, client)
	require.Error(t, err)
	_, err = NewMall(provider, nil)
	require.Error(t, err)
}

func TestMallExecutionBoundsIntentBeforeCallingProvider(t *testing.T) {
	provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
		t.Fatal("invalid intent reached provider")
		return nil, nil
	})
	client := checkClientFunc(func(context.Context, *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		t.Fatal("invalid intent reached check")
		return nil, nil
	})
	e, err := NewMall(provider, client)
	require.NoError(t, err)
	for _, mutate := range []func(*agent.Intent){
		func(i *agent.Intent) { i.Keywords = make([]string, agent.MaxKeywords+1) },
		func(i *agent.Intent) { i.Keywords = []string{strings.Repeat("x", agent.MaxKeywordRunes+1)} },
		func(i *agent.Intent) { i.Keywords = []string{string([]byte{0xff})} },
		func(i *agent.Intent) { i.Demand.Required = make([]string, 17) },
		func(i *agent.Intent) { i.Preferences = make([]string, 17) },
	} {
		intent := mallIntent()
		mutate(&intent)
		result, err := e.Run(context.Background(), intent)
		require.ErrorIs(t, err, agent.ErrInvalidInput)
		require.Nil(t, result)
	}
}

func TestMallExecutionMergesWholeRetrievalBeforeHydration(t *testing.T) {
	provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
		raw := mallRaw()
		copy := raw[0]
		copy.Stock, copy.PriceCents = 0, 0 // stale invalid facts must be replaced by the actual check
		raw = append(raw, copy)
		return raw, nil
	})
	checks := 0
	client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
		checks++
		require.Equal(t, []string{"a", "b", "c"}, req.SkuIds)
		return mallFacts(req), nil
	})
	e, err := NewMall(provider, client)
	require.NoError(t, err)
	r, err := e.Run(context.Background(), mallIntent())
	require.NoError(t, err)
	require.EqualValues(t, 150, r.TotalPriceCents)
	require.Equal(t, 2, checks)
}

func TestMallExecutionCannotSatisfyCategoriesUsingContradictorySPUFacts(t *testing.T) {
	for _, attempt := range []int{1, 2} {
		provider := rankedSourceFunc(func(context.Context, tools.SearchProductsReq, int) ([]agent.ProductCandidate, error) {
			raw := mallRaw()
			raw[1].Evidence.ProductID = raw[0].Evidence.ProductID
			return raw, nil
		})
		checks := 0
		client := checkClientFunc(func(_ context.Context, req *pb.CheckProductCandidatesReq) (*pb.CheckProductCandidatesResp, error) {
			checks++
			r := mallFacts(req)
			r.Results[1].Facts.ProductId = r.Results[0].Facts.ProductId
			if checks == attempt {
				r.Results[1].Facts.DemandCategory.Code = "mouse"
				r.Results[1].Facts.DemandCategory.Revision++
			}
			return r, nil
		})
		e, err := NewMall(provider, client)
		require.NoError(t, err)
		r, err := e.Run(context.Background(), mallIntent())
		require.Error(t, err)
		require.Nil(t, r)
		require.Equal(t, attempt, checks)
	}
}
