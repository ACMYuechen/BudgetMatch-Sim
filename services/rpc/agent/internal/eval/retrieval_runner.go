package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RetrievalMetadata struct {
	DatasetVersion   string `json:"dataset_version"`
	DatasetSHA256    string `json:"dataset_sha256"`
	Protocol         string `json:"protocol"`
	ProtocolSHA256   string `json:"protocol_sha256"`
	CodeRevision     string `json:"code_revision"`
	Provenance       string `json:"provenance"`
	AnnotationStatus string `json:"annotation_status"`
	TopK             int    `json:"top_k"`
	InitialK         int    `json:"initial_k"`
	MaxK             int    `json:"max_k"`
	RRFConstant      int    `json:"rrf_constant"`
	TimeoutMillis    int    `json:"timeout_millis"`
	GoVersion        string `json:"go_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	GOMAXPROCS       int    `json:"gomaxprocs"`
	CaseConcurrency  int    `json:"case_concurrency"`
}

type RetrievalRead struct {
	Lane   string `json:"lane"`
	Window int    `json:"window"`
}

type RetrievalOutcome struct {
	ID             string                   `json:"id"`
	Kind           string                   `json:"kind"`
	Outcome        string                   `json:"outcome"`
	Expected       string                   `json:"expected"`
	OutcomeMatched bool                     `json:"outcome_matched"`
	ReturnedIDs    []string                 `json:"returned_ids"`
	RawCount       int                      `json:"raw_count"`
	RelevantHits   int                      `json:"relevant_hits"`
	RelevantTotal  int                      `json:"relevant_total"`
	NDCG           *float64                 `json:"ndcg_at_k"`
	Violations     []string                 `json:"violations"`
	Reads          []RetrievalRead          `json:"reads"`
	Expanded       bool                     `json:"expanded"`
	Fallback       bool                     `json:"fallback"`
	Degraded       bool                     `json:"degraded"`
	Rankings       []agent.CandidateRanking `json:"rankings"`
	ReplayMS       float64                  `json:"replay_ms"`
}

type RetrievalStrategyReport struct {
	ID      string             `json:"id"`
	Summary RetrievalSummary   `json:"summary"`
	Cases   []RetrievalOutcome `json:"cases"`
}

type RetrievalComparison struct {
	Baseline  string   `json:"baseline"`
	Improved  []string `json:"ndcg_improved"`
	Regressed []string `json:"ndcg_regressed"`
	Tied      []string `json:"ndcg_tied"`
}

type RetrievalReport struct {
	SchemaVersion  int                       `json:"schema_version"`
	Metadata       RetrievalMetadata         `json:"metadata"`
	Strategies     []RetrievalStrategyReport `json:"strategies"`
	Comparisons    []RetrievalComparison     `json:"hybrid_comparisons"`
	GatePassed     bool                      `json:"gate_passed"`
	RealEmbedding  string                    `json:"real_embedding"`
	RealMall       string                    `json:"real_mall"`
	FinalLiveCheck string                    `json:"final_live_check"`
	RealModel      string                    `json:"real_model"`
	TokenUsage     *int64                    `json:"token_usage"`
	CostUSD        *float64                  `json:"cost_usd"`
}

func RunRetrieval(ctx context.Context, d RetrievalDataset, revision string) (RetrievalReport, error) {
	if err := d.fixture.validate(); err != nil {
		return RetrievalReport{}, err
	}
	if revision == "" {
		revision = "unknown"
	}
	f := d.fixture
	// Bind algorithm/protocol parameters separately from the raw fixture hash.
	protocol, _ := json.Marshal(struct {
		Version    string
		Strategies [4]string
		Config     rag.Config
		RRF        int
	}{retrievalProtocol, retrievalStrategies, f.config(), rag.RRFConstant})
	r := RetrievalReport{SchemaVersion: 1, GatePassed: true, RealEmbedding: "not_run", RealMall: "not_run", RealModel: "not_run", FinalLiveCheck: "not_run",
		Metadata: RetrievalMetadata{DatasetVersion: f.Version, DatasetSHA256: d.sha256, Protocol: retrievalProtocol,
			ProtocolSHA256: fmt.Sprintf("%x", sha256.Sum256(protocol)), CodeRevision: revision, Provenance: f.Provenance, AnnotationStatus: f.AnnotationStatus,
			TopK: f.TopK, InitialK: f.InitialK, MaxK: f.MaxK, RRFConstant: rag.RRFConstant, TimeoutMillis: f.config().Retrieval.TimeoutMillis,
			GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0), CaseConcurrency: 1}}
	products := make(map[string]RetrievalProduct, len(f.Products))
	for _, p := range f.Products {
		products[p.ID] = p
	}
	for _, strategy := range retrievalStrategies {
		s := RetrievalStrategyReport{ID: strategy}
		for _, c := range f.Cases {
			if err := ctx.Err(); err != nil {
				return RetrievalReport{}, err
			}
			out, err := runRetrievalCase(ctx, strategy, f.config(), products, c)
			if err != nil {
				return RetrievalReport{}, err
			}
			s.Cases = append(s.Cases, out)
		}
		s.Summary = summarizeRetrieval(s.Cases, f.TopK)
		r.GatePassed = r.GatePassed && s.Summary.GatePassed
		r.Strategies = append(r.Strategies, s)
	}
	hybrid := r.Strategies[len(r.Strategies)-1]
	for _, baseline := range r.Strategies[:len(r.Strategies)-1] {
		comparison := RetrievalComparison{Baseline: baseline.ID, Improved: []string{}, Regressed: []string{}, Tied: []string{}}
		for i, a := range hybrid.Cases {
			b := baseline.Cases[i]
			if a.Kind != "quality" || a.NDCG == nil {
				continue
			}
			switch delta := *a.NDCG - *b.NDCG; {
			case delta > 1e-12:
				comparison.Improved = append(comparison.Improved, a.ID)
			case delta < -1e-12:
				comparison.Regressed = append(comparison.Regressed, a.ID)
			default:
				comparison.Tied = append(comparison.Tied, a.ID)
			}
		}
		r.Comparisons = append(r.Comparisons, comparison)
	}
	return r, nil
}

func runRetrievalCase(ctx context.Context, strategy string, cfg rag.Config, products map[string]RetrievalProduct, c RetrievalCase) (RetrievalOutcome, error) {
	// Only source rankings/faults/catalog reach adapters, never relevance labels or
	// expected outcomes. Each strategy/case gets a fresh recorder and provider.
	backend := &rankReplay{products: products, keyword: c.Keyword, vector: c.Vector, initialK: cfg.Retrieval.InitialK, maxK: cfg.Retrieval.MaxK}
	keyword := replayKeyword{backend: backend, disabled: strategy == "vector_only"}
	vector := replayVector{backend: backend, disabled: strategy == "keyword_only"}
	var provider tools.ProductProvider
	if strategy == "vector_first" {
		provider = tools.NewRAGProductProvider(vector, keyword, cfg.TopK)
	} else {
		var err error
		provider, err = tools.NewHybridProductProvider(vector, keyword, cfg)
		if err != nil {
			return RetrievalOutcome{}, err
		}
	}
	start := time.Now()
	search, err := tools.SearchWithTrace(ctx, provider, tools.SearchProductsReq{Query: c.Input.Query, BudgetCents: c.Input.BudgetCents, MaxItems: c.Input.MaxItems})
	elapsed := time.Since(start)
	if stopped := ctx.Err(); stopped != nil {
		return RetrievalOutcome{}, stopped
	}
	out := assessRetrieval(products, c, search.Candidates, err, cfg.TopK)
	out.ReplayMS = float64(elapsed) / float64(time.Millisecond)
	out.Reads = backend.snapshot()
	keywordCalls, vectorCalls := 0, 0
	for _, read := range out.Reads {
		if read.Lane == "keyword" {
			keywordCalls++
		} else {
			vectorCalls++
		}
		if read.Window < 1 || read.Window > cfg.Retrieval.MaxK {
			out.Violations = append(out.Violations, "read_window_exceeded")
		}
	}
	maxCalls := 2
	if strategy == "vector_first" {
		maxCalls = 1
		out.Fallback = keywordCalls > 0 && vectorCalls > 0
		out.Degraded = err == nil && out.Fallback && c.Vector.Error != ""
	}
	missingSource := (strategy != "keyword_only" && vectorCalls == 0) ||
		(strategy != "vector_only" && strategy != "vector_first" && keywordCalls == 0)
	if missingSource || keywordCalls > maxCalls || vectorCalls > maxCalls || (strategy == "keyword_only" && vectorCalls != 0) || (strategy == "vector_only" && keywordCalls != 0) {
		out.Violations = append(out.Violations, "source_call_bound_exceeded")
	}
	if out.RawCount > cfg.Retrieval.MaxK {
		out.Violations = append(out.Violations, "raw_window_exceeded")
	}
	for _, call := range search.Calls {
		if call.Name == "retrieval.expand" {
			out.Expanded = true
		}
		if err == nil && !call.Success && (call.Name == "retrieval.keyword" || call.Name == "retrieval.vector") {
			out.Degraded = true
		}
	}
	expected := "ok"
	if c.Kind == "fault" {
		expected = c.ExpectedOutcomes[strategy]
	}
	out.Expected = expected
	out.OutcomeMatched = out.Outcome == expected
	zeroUnsafeRetrievalQuality(&out)
	return out, nil
}

type rankReplay struct {
	products        map[string]RetrievalProduct
	keyword, vector RetrievalLane
	initialK, maxK  int
	mu              sync.Mutex
	reads           []RetrievalRead
}

func (r *rankReplay) read(ctx context.Context, lane string, k int) ([]agent.ProductCandidate, error) {
	r.mu.Lock()
	r.reads = append(r.reads, RetrievalRead{Lane: lane, Window: k})
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if k < 1 || k > r.maxK {
		return nil, status.Error(codes.Unavailable, "replay window unavailable")
	}
	source := r.keyword
	if lane == "vector" {
		source = r.vector
	}
	code := source.Error
	if code == "" && k > r.initialK {
		code = source.WiderError
	}
	if code != "" {
		return nil, retrievalFault(code)
	}
	var out []agent.ProductCandidate
	for _, id := range source.IDs[:min(k, len(source.IDs))] {
		p := r.products[id]
		out = append(out, agent.ProductCandidate{Id: p.ID, Name: p.Name, PriceCents: p.PriceCents, Stock: p.Stock, Sold: p.Sold, Source: "mall",
			Evidence: agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, ProductID: p.ProductID}})
	}
	return out, nil
}

func (r *rankReplay) snapshot() []RetrievalRead {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]RetrievalRead{}, r.reads...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		return out[i].Window < out[j].Window
	})
	return out
}

type replayKeyword struct {
	backend  *rankReplay
	disabled bool
}

func (replayKeyword) Name() string { return "eval.rank_replay" }
func (p replayKeyword) SearchRankedProducts(ctx context.Context, _ tools.SearchProductsReq, k int) ([]agent.ProductCandidate, error) {
	if p.disabled {
		return nil, nil
	}
	return p.backend.read(ctx, "keyword", k)
}
func (p replayKeyword) SearchProducts(ctx context.Context, req tools.SearchProductsReq) ([]agent.ProductCandidate, error) {
	// Legacy fallback has no window argument: replay the whole bounded keyword
	// list, not Mall's real pagination. Reads expose this different work budget.
	candidates, err := p.SearchRankedProducts(ctx, req, p.backend.maxK)
	if err != nil {
		return nil, err
	}
	return replayEligible(candidates, req.BudgetCents), nil
}

type replayVector struct {
	backend  *rankReplay
	disabled bool
}

func (v replayVector) Retrieve(ctx context.Context, _ string, options ...retriever.Option) ([]*schema.Document, error) {
	if v.disabled {
		return nil, nil
	}
	k := retriever.GetCommonOptions(&retriever.Options{}, options...).TopK
	if k == nil {
		return nil, status.Error(codes.Unavailable, "missing replay window")
	}
	candidates, err := v.backend.read(ctx, "vector", *k)
	if err != nil {
		return nil, err
	}
	var docs []*schema.Document
	for i, c := range candidates {
		docs = append(docs, rag.NewCandidateDocument(c.Id, c.Name, rag.CandidateMetadata{ProductId: c.Evidence.ProductID,
			Name: c.Name, PriceCents: c.PriceCents, Stock: c.Stock, Sold: c.Sold}).WithScore(1/float64(i+1)))
	}
	return docs, nil
}

func replayEligible(candidates []agent.ProductCandidate, budget int64) []agent.ProductCandidate {
	var out []agent.ProductCandidate
	for _, c := range agent.NormalizeCandidates(candidates) {
		if c.PriceCents <= budget {
			out = append(out, c)
		}
	}
	return out
}

func retrievalFault(code string) error {
	c := map[string]codes.Code{"unavailable": codes.Unavailable, "permission_denied": codes.PermissionDenied,
		"unauthenticated": codes.Unauthenticated, "canceled": codes.Canceled, "deadline_exceeded": codes.DeadlineExceeded}[code]
	return status.Error(c, "injected replay fault")
}

func retrievalOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	switch status.Code(err) {
	case codes.Unavailable:
		return "unavailable"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.Canceled:
		return "canceled"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "unexpected_error"
	}
}
