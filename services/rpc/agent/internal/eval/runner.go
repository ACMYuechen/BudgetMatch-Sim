package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	recommendagent "budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Options struct {
	Split    string
	TopK     int
	Revision string
}

type Metadata struct {
	SnapshotVersion  string `json:"snapshot_version"`
	SnapshotSHA256   string `json:"snapshot_sha256"`
	CasesSHA256      string `json:"cases_sha256"`
	AnnotationStatus string `json:"annotation_status"`
	CodeRevision     string `json:"code_revision"`
	StrategyVersion  string `json:"strategy_version"`
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	Embedding        string `json:"embedding"`
	GoVersion        string `json:"go_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	LogicalCPUs      int    `json:"logical_cpus"`
	Concurrency      int    `json:"concurrency"`
	CachePolicy      string `json:"cache_policy"`
	Split            string `json:"split"`
	TopK             int    `json:"top_k"`
}

type BaselineStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type Timing struct {
	RecommendationMS float64 `json:"recommendation_ms"`
	RetrievalMS      float64 `json:"retrieval_ms"`
	ReplayMS         float64 `json:"replay_ms"`
}
type CaseResult struct {
	ID                       string   `json:"id"`
	Split                    string   `json:"split"`
	Scenario                 string   `json:"scenario"`
	Status                   string   `json:"status"`
	ErrorCode                string   `json:"error_code,omitempty"`
	ExpectedStatus           string   `json:"expected_status"`
	Feasibility              string   `json:"feasibility"`
	OutcomeMatched           bool     `json:"outcome_matched"`
	TaskSuccess              bool     `json:"task_success"`
	SelectedIDs              []string `json:"selected_ids"`
	RetrievedIDs             []string `json:"retrieved_ids"`
	TotalPriceCents          int64    `json:"total_price_cents"`
	Violations               []string `json:"violations"`
	FactConsistent           bool     `json:"fact_consistent"`
	RequirementsMet          int      `json:"requirements_met"`
	RequirementsTotal        int      `json:"requirements_total"`
	RelevantHits             int      `json:"relevant_hits"`
	RelevantTotal            int      `json:"relevant_total"`
	Fallback                 bool     `json:"fallback"`
	ProviderCalls            int      `json:"provider_calls"`
	FaultPrimaryCalls        int      `json:"fault_primary_calls"`
	ModelCalls               int      `json:"model_calls"`
	TokenUsage               *int64   `json:"token_usage"` // 未调用模型；null 不是提供商报告的零消耗
	PersistenceOK            bool     `json:"persistence_ok"`
	ReplayChecked            bool     `json:"replay_checked"`
	ReplayOK                 bool     `json:"replay_ok"`
	ReplayExtraProviderCalls int      `json:"replay_extra_provider_calls"`
	ReplayExtraPrimaryCalls  int      `json:"replay_extra_primary_calls"`
	ReplayExtraTurns         int64    `json:"replay_extra_turns"`
	Timing                   Timing   `json:"timing"`
}

type Report struct {
	SchemaVersion int                `json:"schema_version"`
	Metadata      Metadata           `json:"metadata"`
	Baselines     []BaselineStatus   `json:"baselines"`
	Summary       Summary            `json:"summary"`
	BySplit       map[string]Summary `json:"by_split"`
	ByScenario    map[string]Summary `json:"by_scenario"`
	Cases         []CaseResult       `json:"cases"`
	Comparison    *Comparison        `json:"comparison,omitempty"`
}

func Run(ctx context.Context, d Dataset, opts Options) (Report, error) {
	if err := d.Validate(); err != nil {
		return Report{}, err
	}
	if opts.Split == "" {
		opts.Split = "all"
	}
	if opts.TopK == 0 {
		opts.TopK = 10
	}
	if opts.Split != "all" && opts.Split != "dev" && opts.Split != "holdout" {
		return Report{}, errors.New("split must be all, dev or holdout")
	}
	if opts.TopK < 1 || opts.TopK > 256 {
		return Report{}, errors.New("top-k must be within 1..256")
	}
	if opts.Revision == "" {
		opts.Revision = "unknown"
	}
	r := Report{SchemaVersion: 1, Metadata: Metadata{
		SnapshotVersion: d.Snapshot.Version, SnapshotSHA256: d.SnapshotSHA256, CasesSHA256: d.CasesSHA256, AnnotationStatus: d.Snapshot.AnnotationStatus,
		CodeRevision: opts.Revision, StrategyVersion: "offline-keyword-rule-v1", Model: "not_used", Prompt: "not_used", Embedding: "not_run",
		GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(), Concurrency: 1,
		CachePolicy: "fresh in-memory conversation per case; same process; no explicit warmup; no remote cache", Split: opts.Split, TopK: opts.TopK,
	}, Baselines: []BaselineStatus{
		{ID: "keyword_rule", Status: "executed", Reason: "fixed snapshot adapter + production Planner/Service/BundleSelector; no Mall RPC"},
		{ID: "vector_rule", Status: "not_run", Reason: "real embedding and retrieval comparison not configured"},
		{ID: "vector_react", Status: "not_run", Reason: "real embedding/model evaluation requires explicit data and cost authorization"},
	}}
	for _, c := range d.Cases {
		if opts.Split != "all" && c.Split != opts.Split {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		out, err := runCase(ctx, d.Snapshot, c, opts.TopK)
		if err != nil {
			return Report{}, fmt.Errorf("evaluation case %s: %w", c.ID, err)
		}
		r.Cases = append(r.Cases, out)
	}
	if len(r.Cases) == 0 {
		return Report{}, errors.New("selected split has no cases")
	}
	r.Summary = Summarize(r.Cases)
	r.BySplit = map[string]Summary{}
	r.ByScenario = map[string]Summary{}
	splits, scenarios := map[string][]CaseResult{}, map[string][]CaseResult{}
	for _, c := range r.Cases {
		splits[c.Split] = append(splits[c.Split], c)
		scenarios[c.Scenario] = append(scenarios[c.Scenario], c)
	}
	for k, cases := range splits {
		r.BySplit[k] = Summarize(cases)
	}
	for k, cases := range scenarios {
		r.ByScenario[k] = Summarize(cases)
	}
	return r, nil
}

// snapshotProvider 只是离线 I/O 适配层。按命中关键词数降序/ID 升序取 TopK，
// 不读取答案标注、不做无匹配全量回退；不得等同于真实 Mall 或语义检索质量。
type snapshotProvider struct {
	products []Product
	topK     int
	fault    string
	calls    int
	ids      []string
	elapsed  time.Duration
}

func (p *snapshotProvider) Name() string { return "eval.snapshot" }
func (p *snapshotProvider) SearchProducts(ctx context.Context, req tools.SearchProductsReq) ([]agent.ProductCandidate, error) {
	p.calls++
	start := time.Now()
	defer func() { p.elapsed += time.Since(start) }()
	p.ids = nil
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch p.fault {
	case "provider_unavailable":
		return nil, status.Error(codes.Unavailable, "offline injected provider failure")
	case "provider_permission":
		return nil, status.Error(codes.PermissionDenied, "offline injected provider denial")
	}
	type match struct {
		product Product
		score   int
	}
	var matches []match
	keywords := req.Keywords
	if len(keywords) == 0 {
		keywords = []string{req.Query}
	}
	for _, product := range p.products {
		if !product.Active || product.Stock <= 0 || product.PriceCents > req.BudgetCents {
			continue
		}
		text := strings.ToLower(product.Name + " " + product.Category + " " + strings.Join(product.Tags, " "))
		hits := 0
		for _, keyword := range keywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword != "" && strings.Contains(text, keyword) {
				hits++
			}
		}
		if hits > 0 {
			matches = append(matches, match{product, hits})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].product.ID < matches[j].product.ID
	})
	var out []agent.ProductCandidate
	for _, m := range matches[:min(len(matches), p.topK)] {
		product := m.product
		p.ids = append(p.ids, product.ID)
		out = append(out, agent.ProductCandidate{Id: product.ID, Name: product.Name, Category: product.Category, Source: "eval_snapshot", PriceCents: product.PriceCents, Stock: product.Stock, Sold: product.Sold, Tags: append([]string(nil), product.Tags...)})
	}
	return out, nil
}

// faultPrimary 只注入编排依赖故障，不冒充 Fake Model 或真实 ReAct。
type faultPrimary struct{ calls int }

func (p *faultPrimary) Name() string { return "eval.fault_primary" }
func (p *faultPrimary) Run(context.Context, agent.Input) (*agent.Result, error) {
	p.calls++
	return nil, status.Error(codes.Unavailable, "offline injected primary failure")
}

func runCase(ctx context.Context, snapshot Snapshot, c Case, k int) (CaseResult, error) {
	mem := memory.NewInMemory(memory.Conf{})
	p := &snapshotProvider{products: snapshot.Products, topK: k}
	rule := recommendagent.NewAgent(p, selector.NewBundleSelector()).WithMemory(mem, 20)
	svc := recommendagent.NewService(rule, nil, mem)
	input := func(in TurnInput, turn string) agent.Input {
		return agent.Input{UserId: "eval-user", ConversationId: c.ID, TurnId: turn, Query: in.Query, BudgetCents: in.BudgetCents, MaxItems: in.MaxItems}
	}
	for i, h := range c.History {
		if _, err := svc.Recommend(ctx, input(h, fmt.Sprintf("history-%d", i))); err != nil {
			return CaseResult{}, errors.New("history setup failed")
		}
	}
	before, _, err := mem.GetConversation(ctx, "eval-user", c.ID)
	if err != nil {
		return CaseResult{}, err
	}
	p.calls, p.elapsed, p.ids = 0, 0, nil
	p.fault = c.Fault
	var primary *faultPrimary
	if c.Fault == "primary_unavailable" {
		primary = &faultPrimary{}
		svc = recommendagent.NewService(rule, primary, mem)
	}
	in := input(c.Input, "measured-turn")
	start := time.Now()
	result, runErr := svc.Recommend(ctx, in)
	elapsed := time.Since(start)
	if err := ctx.Err(); err != nil {
		return CaseResult{}, err
	}
	out := Assess(snapshot, c, result, runErr, p.ids)
	out.ProviderCalls = p.calls
	out.Timing.RecommendationMS = float64(elapsed) / float64(time.Millisecond)
	out.Timing.RetrievalMS = float64(p.elapsed) / float64(time.Millisecond)
	if primary != nil {
		out.FaultPrimaryCalls = primary.calls
	}
	after, _, err := mem.GetConversation(ctx, "eval-user", c.ID)
	if err != nil {
		return CaseResult{}, err
	}
	if runErr != nil {
		out.PersistenceOK = after.TurnCount == before.TurnCount
		return out, nil
	}
	out.PersistenceOK = after.TurnCount == before.TurnCount+1
	out.ReplayChecked = true
	start = time.Now()
	replayed, replayErr := svc.Recommend(ctx, in)
	out.Timing.ReplayMS = float64(time.Since(start)) / float64(time.Millisecond)
	if err := ctx.Err(); err != nil {
		return CaseResult{}, err
	}
	afterReplay, _, err := mem.GetConversation(ctx, "eval-user", c.ID)
	if err != nil {
		return CaseResult{}, err
	}
	out.ReplayExtraTurns = afterReplay.TurnCount - after.TurnCount
	out.ReplayExtraProviderCalls = p.calls - out.ProviderCalls
	if primary != nil {
		out.ReplayExtraPrimaryCalls = primary.calls - out.FaultPrimaryCalls
	}
	a, _ := json.Marshal(result)
	b, _ := json.Marshal(replayed)
	out.ReplayOK = replayErr == nil && string(a) == string(b) && out.ReplayExtraTurns == 0 && out.ReplayExtraProviderCalls == 0 && out.ReplayExtraPrimaryCalls == 0
	return out, nil
}
