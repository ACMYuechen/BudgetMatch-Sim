package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
)

const demandProtocol = "same-snapshot-demand-replay-v1"

type DemandStrategy struct {
	ID            string `json:"id"`
	MaxCandidates int    `json:"max_candidates"`
	BeamWidth     int    `json:"beam_width"`
	MaxExpansions int    `json:"max_expansions"`
	TimeBudgetMS  int    `json:"time_budget_ms"`
}

func demandStrategies() []DemandStrategy {
	return []DemandStrategy{
		{ID: "legacy_greedy"},
		{"beam_default", 32, 32, 8192, 100},
		{"beam_width_1", 32, 1, 8192, 100},
		{"beam_window_1", 1, 32, 8192, 100},
		{"beam_expansions_1", 32, 32, 1, 100},
	}
}

func (s DemandStrategy) config() beam.Config {
	return beam.Config{MaxCandidates: s.MaxCandidates, BeamWidth: s.BeamWidth, MaxExpansions: s.MaxExpansions, TimeBudget: time.Duration(s.TimeBudgetMS) * time.Millisecond}
}

type DemandMetadata struct {
	DatasetVersion   string `json:"dataset_version"`
	DatasetSHA256    string `json:"dataset_sha256"`
	Protocol         string `json:"protocol"`
	ProtocolSHA256   string `json:"protocol_sha256"`
	CodeRevision     string `json:"code_revision"`
	Provenance       string `json:"provenance"`
	AnnotationStatus string `json:"annotation_status"`
	GoVersion        string `json:"go_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	GOMAXPROCS       int    `json:"gomaxprocs"`
	Concurrency      int    `json:"concurrency"`
}

type DemandSelectionOutcome struct {
	ID string `json:"id"`
	DemandAssessment
	Oracle       DemandOracle `json:"oracle"`
	OracleMatch  bool         `json:"oracle_optimum_match"`
	MissReason   string       `json:"miss_reason"`
	Stats        *beam.Stats  `json:"search_stats"` // null for the uninstrumented greedy algorithm
	WindowIDs    []string     `json:"window_ids"`
	GateProblems []string     `json:"gate_problems"`
	ReplayMS     float64      `json:"replay_ms"`
}

type DemandSelectionSummary struct {
	Cases               int     `json:"cases"`
	SatisfiableSuccess  Ratio   `json:"satisfiable_success"`
	HardViolationRate   Ratio   `json:"nonempty_hard_violation_rate"`
	RequirementCoverage Ratio   `json:"required_coverage_micro"`
	OptionalCoverage    Ratio   `json:"optional_coverage_micro"`
	OracleOptimumMatch  Ratio   `json:"feasible_oracle_optimum_match"`
	Misses              int     `json:"feasible_misses"`
	LimitedMisses       int     `json:"limited_feasible_misses"`
	SearchLimitedCases  int     `json:"search_limited_cases"`
	InputSnapshots      int     `json:"input_snapshots"`
	SearchExpansions    *int    `json:"search_expansions"`
	ReplayP50MS         float64 `json:"replay_p50_ms"`
	ReplayP95MS         float64 `json:"replay_p95_ms"`
}

type DemandStrategyReport struct {
	Strategy DemandStrategy           `json:"strategy"`
	Summary  DemandSelectionSummary   `json:"summary"`
	Cases    []DemandSelectionOutcome `json:"cases"`
}

type DemandComparison struct {
	Baseline  string   `json:"baseline"`
	Improved  []string `json:"task_success_improved"`
	Regressed []string `json:"task_success_regressed"`
	Tied      []string `json:"task_success_tied"`
}

type DemandReport struct {
	SchemaVersion    int                      `json:"schema_version"`
	Metadata         DemandMetadata           `json:"metadata"`
	Selection        []DemandStrategyReport   `json:"selection"`
	Comparisons      []DemandComparison       `json:"beam_default_comparisons"`
	Execution        []DemandExecutionOutcome `json:"execution"`
	ExecutionSummary DemandExecutionSummary   `json:"execution_summary"`
	GatePassed       bool                     `json:"gate_passed"`
	RealMall         string                   `json:"real_mall"`
	RealModel        string                   `json:"real_model"`
	RealEmbedding    string                   `json:"real_embedding"`
	ModelCalls       int                      `json:"model_calls"`
	TokenUsage       *int64                   `json:"token_usage"`
	CostUSD          *float64                 `json:"cost_usd"`
}

func RunDemand(ctx context.Context, d DemandDataset, revision string) (DemandReport, error) {
	if err := d.fixture.validate(); err != nil {
		return DemandReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return DemandReport{}, err
	}
	if revision == "" {
		revision = "unknown"
	}
	strategies := demandStrategies()
	protocol, _ := json.Marshal(struct {
		Version       string
		Strategies    []DemandStrategy
		MaxOracleSKUs int
		Execution     string
	}{demandProtocol, strategies, 8, "NewMall: one ranked provider read; <=2 classified check calls; no model; 3s execution/2s check"})
	r := DemandReport{SchemaVersion: 1, GatePassed: true, RealMall: "not_run", RealModel: "not_run", RealEmbedding: "not_run",
		Metadata: DemandMetadata{DatasetVersion: d.fixture.Version, DatasetSHA256: d.sha256, Protocol: demandProtocol,
			ProtocolSHA256: fmt.Sprintf("%x", sha256.Sum256(protocol)), CodeRevision: revision, Provenance: d.fixture.Provenance,
			AnnotationStatus: d.fixture.AnnotationStatus, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
			GOMAXPROCS: runtime.GOMAXPROCS(0), Concurrency: 1}}
	oracles := make([]DemandOracle, len(d.fixture.Selection))
	for i, c := range d.fixture.Selection {
		var err error
		oracles[i], err = demandOracle(ctx, c.State, d.products(c.Candidates))
		if err != nil {
			return DemandReport{}, err
		}
	}
	for _, strategy := range strategies {
		s := DemandStrategyReport{Strategy: strategy}
		for i, c := range d.fixture.Selection {
			out, err := runDemandSelection(ctx, strategy, c, d.products(c.Candidates), oracles[i])
			if err != nil {
				return DemandReport{}, err
			}
			r.GatePassed = r.GatePassed && len(out.GateProblems) == 0
			s.Cases = append(s.Cases, out)
		}
		s.Summary = summarizeDemandSelection(s.Cases, d.fixture.Selection)
		r.Selection = append(r.Selection, s)
	}
	for i, baseline := range r.Selection {
		if i == 1 {
			continue
		}
		comparison := DemandComparison{Baseline: baseline.Strategy.ID, Improved: []string{}, Regressed: []string{}, Tied: []string{}}
		for j, after := range r.Selection[1].Cases {
			before := baseline.Cases[j]
			switch {
			case after.TaskSuccess && !before.TaskSuccess:
				comparison.Improved = append(comparison.Improved, after.ID)
			case !after.TaskSuccess && before.TaskSuccess:
				comparison.Regressed = append(comparison.Regressed, after.ID)
			default:
				comparison.Tied = append(comparison.Tied, after.ID)
			}
		}
		r.Comparisons = append(r.Comparisons, comparison)
	}
	for _, c := range d.fixture.Execution {
		var base DemandSelectionCase
		for _, s := range d.fixture.Selection {
			if s.ID == c.SelectionCase {
				base = s
				break
			}
		}
		out, err := runDemandExecution(ctx, c, base, d.products(base.Candidates))
		if err != nil {
			return DemandReport{}, err
		}
		r.GatePassed = r.GatePassed && len(out.GateProblems) == 0 && out.OutcomeMatched
		r.Execution = append(r.Execution, out)
	}
	r.ExecutionSummary = summarizeDemandExecution(r.Execution)
	return r, nil
}

func demandCandidates(products []DemandProduct) []agent.ProductCandidate {
	out := make([]agent.ProductCandidate, 0, len(products))
	for _, p := range products {
		c := agent.ProductCandidate{Id: p.ID, Name: "fixture " + p.ID, Category: string(p.Category), Source: "mall",
			PriceCents: p.PriceCents, Stock: p.Stock, Sold: p.Sold,
			Evidence: agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, State: agent.VerificationChecked,
				ProductID: "p-" + p.ID, VerifiedAtUnixMs: 1, // fixed OFFLINE snapshot, never sent to a real verifier
				DemandCategory: agent.DemandCategoryEvidence{Code: string(p.Category), TaxonomyVersion: "mall_demand_taxonomy_v1", Revision: p.Revision}}}
		if p.KeywordRank > 0 || p.VectorRank > 0 {
			var score float64
			if p.KeywordRank > 0 {
				score += 1 / float64(60+p.KeywordRank)
			}
			if p.VectorRank > 0 {
				score += 1 / float64(60+p.VectorRank)
			}
			c.Evidence.Ranking = agent.CandidateRanking{Method: agent.HybridRankingMethod, KeywordRank: p.KeywordRank, VectorRank: p.VectorRank, FusionScore: score}
		}
		out = append(out, c)
	}
	return out
}

// Construction and fixture/oracle assessment are outside the measured interval.
// Every selector receives the same independently allocated snapshot and intent.
func runDemandSelection(ctx context.Context, strategy DemandStrategy, c DemandSelectionCase, products []DemandProduct, oracle DemandOracle) (DemandSelectionOutcome, error) {
	if err := ctx.Err(); err != nil {
		return DemandSelectionOutcome{}, err
	}
	candidates := demandCandidates(products)
	out := DemandSelectionOutcome{ID: c.ID, Oracle: oracle, WindowIDs: []string{}, GateProblems: []string{}}
	var items []agent.BundleItem
	var total int64
	if strategy.ID == "legacy_greedy" {
		selector := recommend.NewBundleSelector()
		// Direct ALGORITHM ablation, not legacy Recommend/API support for Demand.
		// Only the numeric projection is supported by this old algorithm. Keep
		// the production Demand guard intact; apply the full demands in assessment.
		intent := agent.Intent{BudgetCents: c.State.BudgetCents, MaxItems: c.State.MaxItems}
		start := time.Now()
		items, total = selector.Select(candidates, intent)
		out.ReplayMS = float64(time.Since(start)) / float64(time.Millisecond)
		for _, candidate := range candidates {
			out.WindowIDs = append(out.WindowIDs, candidate.Id)
		}
	} else {
		catalog, err := demand.NewMallCatalog(candidates)
		if err != nil {
			return out, err
		}
		selector, err := beam.New(strategy.config())
		if err != nil {
			return out, err
		}
		start := time.Now()
		result, err := selector.Select(ctx, c.State, catalog, candidates)
		out.ReplayMS = float64(time.Since(start)) / float64(time.Millisecond)
		if err != nil {
			return out, err
		}
		out.Stats, total = &result.Stats, result.TotalPriceCents
		for _, selected := range result.Selected {
			items = append(items, agent.BundleItem{Id: selected.Id, Name: selected.Name, Category: selected.Category,
				Source: selected.Source, PriceCents: selected.PriceCents, Stock: selected.Stock})
		}
		for _, candidate := range result.Window {
			out.WindowIDs = append(out.WindowIDs, candidate.Id)
		}
		if result.Stats.Expansions > strategy.MaxExpansions || result.Stats.PeakFrontier > strategy.BeamWidth || len(result.Window) > strategy.MaxCandidates {
			out.GateProblems = append(out.GateProblems, "work_bounds")
		}
		if result.Stats.TimeBudgetReached {
			out.GateProblems = append(out.GateProblems, "time_budget_inconclusive")
		}
		if result.Status == beam.Complete && len(items) == 0 || result.Status != beam.Complete && len(items) > 0 {
			out.GateProblems = append(out.GateProblems, "status_items_mismatch")
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	slices.Sort(out.WindowIDs)
	out.DemandAssessment = assessDemand(c.State, products, items, total)
	out.OracleMatch = oracle.Feasible && out.TaskSuccess && slices.Equal(out.SelectedIDs, oracle.BestIDs)
	if strategy.ID != "legacy_greedy" && len(out.Violations) > 0 {
		out.GateProblems = append(out.GateProblems, "unsafe_selection")
	}
	// The old selector has no structured-category contract. Preserve its known
	// semantic failures as results, while still enforcing its numeric/fact safety.
	if strategy.ID == "legacy_greedy" {
		for _, v := range out.Violations {
			if !slices.Contains([]string{"missing_required", "excluded_category", "unknown_with_exclusions"}, v) {
				out.GateProblems = append(out.GateProblems, v)
			}
		}
	}
	if oracle.Feasible && !out.TaskSuccess {
		out.MissReason = "unexplained"
		switch {
		case len(items) > 0:
			out.MissReason = "invalid_bundle"
		case out.Stats != nil && out.Stats.TimeBudgetReached:
			out.MissReason = "time_budget"
		case out.Stats != nil && out.Stats.CandidateWindowTruncated:
			out.MissReason = "candidate_window"
		case out.Stats != nil && out.Stats.StopReason == "expansion_limit":
			out.MissReason = "expansion_limit"
		case out.Stats != nil && out.Stats.BeamPruned > 0:
			out.MissReason = "beam_pruning"
		}
		if out.MissReason == "unexplained" {
			out.GateProblems = append(out.GateProblems, "unexplained_feasible_miss")
		}
	}
	return out, nil
}

func summarizeDemandSelection(cases []DemandSelectionOutcome, inputs []DemandSelectionCase) DemandSelectionSummary {
	s := DemandSelectionSummary{Cases: len(cases)}
	var feasible, success, nonempty, violations, met, required, optional, optionalTotal, optimal, expansions int
	var durations []float64
	for i, c := range cases {
		s.InputSnapshots += len(inputs[i].Candidates)
		if c.Oracle.Feasible {
			feasible++
			if c.TaskSuccess {
				success++
			} else {
				s.Misses++
			}
		}
		if len(c.SelectedIDs) > 0 {
			nonempty++
			if len(c.Violations) > 0 {
				violations++
			}
		}
		met += c.RequiredMet
		required += c.RequiredTotal
		optional += c.OptionalMet
		optionalTotal += c.OptionalTotal
		if c.OracleMatch {
			optimal++
		}
		if c.Stats != nil {
			s.SearchExpansions = &expansions
			expansions += c.Stats.Expansions
			if c.Stats.SearchLimited {
				s.SearchLimitedCases++
				if c.Oracle.Feasible && !c.TaskSuccess {
					s.LimitedMisses++
				}
			}
		}
		durations = append(durations, c.ReplayMS)
	}
	s.SatisfiableSuccess, s.HardViolationRate = ratio(success, feasible), ratio(violations, nonempty)
	s.RequirementCoverage, s.OptionalCoverage, s.OracleOptimumMatch = ratio(met, required), ratio(optional, optionalTotal), ratio(optimal, feasible)
	s.ReplayP50MS, s.ReplayP95MS = percentile(durations, .5), percentile(durations, .95)
	return s
}
