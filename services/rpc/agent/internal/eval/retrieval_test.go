package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func retrievalFixtureData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/eval/retrieval.v1.json")
	require.NoError(t, err)
	return data
}

func retrievalDataset(t *testing.T) RetrievalDataset {
	t.Helper()
	d, err := LoadRetrieval(bytes.NewReader(retrievalFixtureData(t)))
	require.NoError(t, err)
	return d
}

func retrievalCaseResult(t *testing.T, r RetrievalReport, strategy, id string) RetrievalOutcome {
	t.Helper()
	for _, s := range r.Strategies {
		if s.ID == strategy {
			for _, c := range s.Cases {
				if c.ID == id {
					return c
				}
			}
		}
	}
	t.Fatalf("missing %s/%s", strategy, id)
	return RetrievalOutcome{}
}

func TestRetrievalReplayUsesProductionPoliciesAndRetainsRegressions(t *testing.T) {
	r, err := RunRetrieval(context.Background(), retrievalDataset(t), "test-worktree")
	require.NoError(t, err)
	require.True(t, r.GatePassed, "%+v", r.Strategies)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(retrievalFixtureData(t))), r.Metadata.DatasetSHA256)
	require.Len(t, r.Strategies, 4)
	for _, s := range r.Strategies {
		require.Len(t, s.Cases, 18)
		require.Equal(t, 10, s.Summary.QualityCases)
		require.Equal(t, 8, s.Summary.FaultCases)
		require.Equal(t, 20, s.Summary.RecallAtK.Denominator)
		require.Equal(t, 40, s.Summary.PrecisionAtK.Denominator)
		require.Equal(t, 8, s.Summary.NDCGAtK.Samples)
		require.Equal(t, 2, s.Summary.NoRelevantCases)
	}
	require.Zero(t, r.Strategies[0].Summary.AllWork.VectorCalls)
	require.Zero(t, r.Strategies[1].Summary.AllWork.KeywordCalls)
	for _, c := range r.Comparisons {
		if c.Baseline == "vector_first" {
			require.Contains(t, c.Regressed, "vector_alias")
			require.Contains(t, c.Regressed, "consensus_noise")
			require.Contains(t, c.Improved, "keyword_exact")
		}
	}
	hybrid := retrievalCaseResult(t, r, "hybrid_rrf", "filtered_prefix")
	require.True(t, hybrid.Expanded)
	require.Equal(t, []string{"k1", "k2", "k3", "k4"}, hybrid.ReturnedIDs)
	require.Equal(t, []RetrievalRead{{"keyword", 8}, {"keyword", 16}, {"vector", 8}, {"vector", 16}}, hybrid.Reads)
	baseline := retrievalCaseResult(t, r, "vector_first", "filtered_prefix")
	// The legacy provider accepts a zero-price snapshot before the common guard
	// drops it, so it does not trigger keyword fallback. Preserve this countercase.
	require.Empty(t, baseline.ReturnedIDs)
	require.Equal(t, 1, baseline.RawCount)
	require.False(t, baseline.Fallback)
	require.Equal(t, []RetrievalRead{{"vector", 8}}, baseline.Reads)
	require.Equal(t, "ok", retrievalCaseResult(t, r, "vector_first", "empty_keyword_vector_failed").Outcome)
	require.Equal(t, "unavailable", retrievalCaseResult(t, r, "hybrid_rrf", "empty_keyword_vector_failed").Outcome)
	require.Equal(t, "ok", retrievalCaseResult(t, r, "vector_first", "keyword_denied").Outcome)
	require.Equal(t, "unauthenticated", retrievalCaseResult(t, r, "hybrid_rrf", "keyword_denied").Outcome)
	require.Nil(t, r.TokenUsage)
	require.Nil(t, r.CostUSD)
	for _, state := range []string{r.RealEmbedding, r.RealMall, r.RealModel, r.FinalLiveCheck} {
		require.Equal(t, "not_run", state)
	}
}

func TestRetrievalFixtureValidation(t *testing.T) {
	mutations := map[string]func(*RetrievalFixture){
		"missing labels":    func(f *RetrievalFixture) { f.Cases[0].RelevantSKUs = nil },
		"missing ranks":     func(f *RetrievalFixture) { f.Cases[0].Keyword.IDs = nil },
		"provenance":        func(f *RetrievalFixture) { f.Provenance = "production" },
		"review":            func(f *RetrievalFixture) { f.AnnotationStatus = "accepted" },
		"empty":             func(f *RetrievalFixture) { f.Cases = nil },
		"duplicate product": func(f *RetrievalFixture) { f.Products = append(f.Products, f.Products[0]) },
		"duplicate case":    func(f *RetrievalFixture) { f.Cases = append(f.Cases, f.Cases[0]) },
		"unknown rank":      func(f *RetrievalFixture) { f.Cases[0].Vector.IDs = []string{"unknown"} },
		"unknown label":     func(f *RetrievalFixture) { f.Cases[0].RelevantSKUs = []string{"unknown"} },
		"duplicate label":   func(f *RetrievalFixture) { f.Cases[0].RelevantSKUs = []string{"k1", "k1"} },
		"ineligible label":  func(f *RetrievalFixture) { f.Cases[0].RelevantSKUs = []string{"b1"} },
		"window":            func(f *RetrievalFixture) { f.MaxK = 65 },
		"zero window":       func(f *RetrievalFixture) { f.InitialK = 0 },
		"negative topk":     func(f *RetrievalFixture) { f.TopK = -1 },
		"oversized rank":    func(f *RetrievalFixture) { f.Cases[0].Keyword.IDs = make([]string, 65) },
		"query":             func(f *RetrievalFixture) { f.Cases[0].Input.Query = "" },
		"limits":            func(f *RetrievalFixture) { f.Cases[0].Input.MaxItems = 0 },
		"unknown fault":     func(f *RetrievalFixture) { f.Cases[10].Vector.Error = "PRIVATE_UNKNOWN" },
		"fault in quality":  func(f *RetrievalFixture) { f.Cases[0].Vector.Error = "unavailable" },
		"outcome missing":   func(f *RetrievalFixture) { delete(f.Cases[10].ExpectedOutcomes, "vector_only") },
		"unknown outcome":   func(f *RetrievalFixture) { f.Cases[10].ExpectedOutcomes["vector_only"] = "PRIVATE_UNKNOWN" },
		"fake fault case":   func(f *RetrievalFixture) { f.Cases[10].Vector.Error = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var f RetrievalFixture
			require.NoError(t, json.Unmarshal(retrievalFixtureData(t), &f))
			mutate(&f)
			data, err := json.Marshal(f)
			require.NoError(t, err)
			_, err = LoadRetrieval(bytes.NewReader(data))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "PRIVATE")
		})
	}
	for _, data := range [][]byte{
		[]byte(`{"unknown":"PRIVATE"}`), append(retrievalFixtureData(t), []byte(` {}`)...),
		[]byte(`{"version":"a","\u0076ersion":"b"}`), []byte{'"', 255, '"'},
		[]byte(strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17)), []byte(strings.Repeat(" ", maxDatasetBytes+1)),
	} {
		_, err := LoadRetrieval(bytes.NewReader(data))
		require.Error(t, err)
	}
	for _, ambiguous := range []string{`{"version":"a","version":"b"}`, `{"version":"a","VERSION":"b"}`, `{"version":"a","\u0076ersion":"b"}`} {
		require.False(t, uniqueRetrievalJSON([]byte(ambiguous)))
	}
	require.True(t, uniqueRetrievalJSON([]byte(`{"ids":[],"nested":{"ids":[1,2]}}`)))
}

func TestRetrievalBinaryMetricsAndFailureDenominators(t *testing.T) {
	hits, ndcg := retrievalQuality([]string{"a", "b"}, []string{"a", "b"}, 4)
	require.Equal(t, 2, hits)
	require.InDelta(t, 1, *ndcg, 1e-12)
	hits, ndcg = retrievalQuality([]string{"x", "y", "a"}, []string{"a"}, 4)
	require.Equal(t, 1, hits)
	require.InDelta(t, .5, *ndcg, 1e-12)
	hits, ndcg = retrievalQuality([]string{"a", "a"}, []string{"a", "b"}, 2)
	require.Equal(t, 1, hits)
	require.InDelta(t, 1/(1+1/math.Log2(3)), *ndcg, 1e-12)
	_, ndcg = retrievalQuality(nil, []string{"a"}, 4)
	require.NotNil(t, ndcg)
	require.Zero(t, *ndcg)
	_, ndcg = retrievalQuality([]string{"a"}, nil, 4)
	require.Nil(t, ndcg)
	c := RetrievalCase{ID: "failure", Kind: "quality", RelevantSKUs: []string{"a"}, Input: TurnInput{BudgetCents: 100, MaxItems: 1}}
	o := assessRetrieval(nil, c, nil, status.Error(codes.Unavailable, "private"), 4)
	s := summarizeRetrieval([]RetrievalOutcome{o}, 4)
	require.Equal(t, 1, s.RecallAtK.Denominator)
	require.Equal(t, 4, s.PrecisionAtK.Denominator)
	require.Equal(t, 1, s.NDCGAtK.Samples)
	require.Zero(t, *s.NDCGAtK.Value)
	require.False(t, s.GatePassed)
	zero := summarizeRetrieval(nil, 4)
	require.Nil(t, zero.RecallAtK.Value)
	require.Nil(t, zero.NDCGAtK.Value)
	require.False(t, zero.GatePassed)
}

func TestRetrievalOracleRejectsSelfReportedFacts(t *testing.T) {
	p := RetrievalProduct{ID: "a", ProductID: "parent", Name: "a", PriceCents: 100, Stock: 1}
	c := RetrievalCase{ID: "facts", Kind: "quality", RelevantSKUs: []string{"a"}, Input: TurnInput{BudgetCents: 1000, MaxItems: 1}}
	candidate := agent.ProductCandidate{Id: "a", Name: "a", PriceCents: 1, Stock: 1, Evidence: agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, ProductID: "parent"}}
	out := assessRetrieval(map[string]RetrievalProduct{"a": p}, c, []agent.ProductCandidate{candidate}, nil, 4)
	require.Contains(t, out.Violations, "catalog_fact_mismatch")
	require.Zero(t, out.RelevantHits)
	require.Zero(t, *out.NDCG)
	out = assessRetrieval(map[string]RetrievalProduct{"a": p}, c, []agent.ProductCandidate{candidate}, status.Error(codes.Unavailable, "private"), 4)
	require.Contains(t, out.Violations, "candidates_on_error")
	require.Empty(t, out.ReturnedIDs)
}

func withoutRetrievalTimings(r RetrievalReport) RetrievalReport {
	for i := range r.Strategies {
		r.Strategies[i].Summary.ReplayP50MS = 0
		r.Strategies[i].Summary.ReplayP95MS = 0
		for j := range r.Strategies[i].Cases {
			r.Strategies[i].Cases[j].ReplayMS = 0
		}
	}
	return r
}

func TestRetrievalReplayIsConcurrentSafeAndDeterministicApartFromTiming(t *testing.T) {
	d := retrievalDataset(t)
	want, err := RunRetrieval(context.Background(), d, "test")
	require.NoError(t, err)
	want = withoutRetrievalTimings(want)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := RunRetrieval(context.Background(), d, "test")
			if err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(want, withoutRetrievalTimings(got)) {
				t.Error("non-timing retrieval result changed")
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = RunRetrieval(ctx, d, "test")
	require.ErrorIs(t, err, context.Canceled)
}

func TestRetrievalAdaptersAreBlindToAnnotations(t *testing.T) {
	d := retrievalDataset(t)
	before, err := RunRetrieval(context.Background(), d, "test")
	require.NoError(t, err)
	f := d.fixture
	f.Cases[0].RelevantSKUs = []string{"n1"}
	data, err := json.Marshal(f)
	require.NoError(t, err)
	afterData, err := LoadRetrieval(bytes.NewReader(data))
	require.NoError(t, err)
	after, err := RunRetrieval(context.Background(), afterData, "test")
	require.NoError(t, err)
	require.NotEqual(t, before.Metadata.DatasetSHA256, after.Metadata.DatasetSHA256)
	require.Equal(t, before.Metadata.ProtocolSHA256, after.Metadata.ProtocolSHA256)
	for i, s := range before.Strategies {
		for j, c := range s.Cases {
			require.Equal(t, c.ReturnedIDs, after.Strategies[i].Cases[j].ReturnedIDs)
			require.Equal(t, c.Reads, after.Strategies[i].Cases[j].Reads)
		}
	}
}

func TestArchivedRetrievalReportMatchesFixtureMetricsAndMarkdown(t *testing.T) {
	d := retrievalDataset(t)
	data, err := os.ReadFile("../../testdata/eval/retrieval-baseline.v1/report.json")
	require.NoError(t, err)
	var report RetrievalReport
	require.NoError(t, decodeStrict(data, &report))
	require.Equal(t, 1, report.SchemaVersion)
	require.Equal(t, d.sha256, report.Metadata.DatasetSHA256)
	require.Equal(t, d.fixture.TopK, report.Metadata.TopK)
	require.Equal(t, "0d4c823+M3.3b-worktree", report.Metadata.CodeRevision)
	require.True(t, report.GatePassed)
	require.Len(t, report.Strategies, 4)
	for _, strategy := range report.Strategies {
		require.Len(t, strategy.Cases, len(d.fixture.Cases))
		for i, c := range strategy.Cases {
			fixture := d.fixture.Cases[i]
			require.Equal(t, fixture.ID, c.ID)
			require.Equal(t, fixture.Kind, c.Kind)
			require.Empty(t, c.Violations)
			hits, ndcg := retrievalQuality(c.ReturnedIDs, fixture.RelevantSKUs, d.fixture.TopK)
			require.Equal(t, hits, c.RelevantHits)
			if ndcg == nil {
				require.Nil(t, c.NDCG)
			} else {
				require.NotNil(t, c.NDCG)
				// Floating-point logarithms may differ by a final bit across targets.
				require.InDelta(t, *ndcg, *c.NDCG, 1e-12)
			}
			require.Equal(t, len(fixture.RelevantSKUs), c.RelevantTotal)
			expected := "ok"
			if fixture.Kind == "fault" {
				expected = fixture.ExpectedOutcomes[strategy.ID]
			}
			require.Equal(t, expected, c.Outcome)
			require.Equal(t, expected, c.Expected)
			require.True(t, c.OutcomeMatched)
		}
		require.Equal(t, summarizeRetrieval(strategy.Cases, d.fixture.TopK), strategy.Summary)
	}
	md, err := os.ReadFile("../../testdata/eval/retrieval-baseline.v1/report.md")
	require.NoError(t, err)
	require.Equal(t, RetrievalMarkdown(report), string(md))
	// Do not compare frozen results with the current provider: a later fix must
	// be able to produce a NEW report while keeping this original counterexample.
}
