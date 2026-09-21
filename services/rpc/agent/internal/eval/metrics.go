package eval

import (
	"errors"
	"math"
	"sort"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/safety"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Ratio 保留明确分母；无有效样本时 value=null，不能误记为 100% 或 0%。
type Ratio struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
}

func ratio(n, d int) Ratio {
	r := Ratio{Numerator: n, Denominator: d}
	if d > 0 {
		v := float64(n) / float64(d)
		r.Value = &v
	}
	return r
}

type Summary struct {
	Requests              int            `json:"requests"`
	Completed             int            `json:"completed"`
	HardViolationRequests int            `json:"hard_violation_requests"`
	HardViolationRate     Ratio          `json:"hard_violation_rate"`
	OutcomeAccuracy       Ratio          `json:"outcome_accuracy"`
	SatisfiableSuccess    Ratio          `json:"satisfiable_success"`
	RequirementCoverage   Ratio          `json:"requirement_coverage"`
	RecallAtK             Ratio          `json:"recall_at_k_micro"`
	NoRelevantCases       int            `json:"no_relevant_cases"`
	FactConsistency       Ratio          `json:"fact_consistency"`
	FallbackRate          Ratio          `json:"fallback_rate"`
	ReplaySuccess         Ratio          `json:"replay_success"`
	PersistenceSuccess    Ratio          `json:"persistence_success"`
	ProviderCalls         int            `json:"provider_calls"`
	FaultPrimaryCalls     int            `json:"fault_primary_calls"`
	ModelCalls            int            `json:"model_calls"`
	LatencyP50MS          float64        `json:"latency_p50_ms"`
	LatencyP95MS          float64        `json:"latency_p95_ms"`
	ErrorCounts           map[string]int `json:"error_counts"`
	GatePassed            bool           `json:"gate_passed"`
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, agent.ErrBudgetCurrency):
		return "budget_currency"
	case errors.Is(err, agent.ErrBudgetText):
		return "budget_text"
	case errors.Is(err, agent.ErrItemLimitText):
		return "item_limit_text"
	}
	if status.Code(err) == codes.PermissionDenied {
		return "permission_denied"
	}
	if status.Code(err) == codes.Unavailable {
		return "execution_failed"
	}
	return safety.ErrorCode(err)
}

// Assess 使用标注中的限额与固定目录核对，不以被测系统自报的候选/限额当作答案。
func Assess(snapshot Snapshot, c Case, result *agent.Result, err error, retrieved []string) CaseResult {
	e := c.Expected
	o := CaseResult{ID: c.ID, Split: c.Split, Scenario: c.Scenario, Status: "completed", ExpectedStatus: e.Status, Feasibility: e.Feasibility,
		SelectedIDs: []string{}, RetrievedIDs: append([]string{}, retrieved...), Violations: []string{}, RequirementsTotal: len(e.Requirements), RelevantTotal: len(e.RelevantSKUs)}
	seenRetrieved := map[string]bool{}
	for _, id := range retrieved {
		if !seenRetrieved[id] && contains(e.RelevantSKUs, id) {
			o.RelevantHits++
		}
		seenRetrieved[id] = true
	}
	if err != nil {
		o.Status = "error"
		o.ErrorCode = errorCode(err)
		if errors.Is(err, agent.ErrInvalidInput) || errors.Is(err, agent.ErrContextTooLarge) {
			o.Status = "rejected"
		}
		o.OutcomeMatched = o.Status == e.Status && o.ErrorCode == e.ErrorCode && !e.Fallback
		return o
	}
	violate := func(code string) {
		if !contains(o.Violations, code) {
			o.Violations = append(o.Violations, code)
		}
	}
	if result == nil {
		violate("nil_completed_result")
		return o
	}
	if e.Status != "completed" {
		violate("unexpected_completion")
	}
	if result.Intent.BudgetCents != e.BudgetCents || result.Intent.MaxItems != e.MaxItems {
		violate("resolved_limits_mismatch")
	}
	if e.MaxItems <= 0 || len(result.Items) > int(e.MaxItems) {
		violate("item_limit")
	}
	products := map[string]Product{}
	for _, p := range snapshot.Products {
		products[p.ID] = p
	}
	selected := map[string]bool{}
	o.FactConsistent = true
	acceptable := true
	var total int64
	for _, item := range result.Items {
		o.SelectedIDs = append(o.SelectedIDs, item.Id)
		if selected[item.Id] {
			violate("duplicate_sku")
			o.FactConsistent = false
		}
		selected[item.Id] = true
		p, exists := products[item.Id]
		if !exists || !p.Active || p.Stock <= 0 || p.PriceCents != item.PriceCents || p.Stock != item.Stock || p.Name != item.Name || p.Category != item.Category || item.Source != "eval_snapshot" {
			violate("snapshot_fact_mismatch")
			o.FactConsistent = false
		}
		if item.PriceCents <= 0 || item.PriceCents > agent.MaxBudgetCents || total > agent.MaxBudgetCents-item.PriceCents {
			violate("invalid_price_or_overflow")
			o.FactConsistent = false
		} else {
			total += item.PriceCents
		}
		if math.IsNaN(item.Score) || math.IsInf(item.Score, 0) {
			violate("invalid_score")
		}
		if !contains(e.AcceptableSKUs, item.Id) {
			acceptable = false
		}
	}
	o.TotalPriceCents = result.TotalPriceCents
	if total != result.TotalPriceCents {
		violate("incorrect_total")
		o.FactConsistent = false
	}
	if total > e.BudgetCents || result.TotalPriceCents > e.BudgetCents {
		violate("budget_limit")
	}
	for _, r := range e.Requirements {
		if len(o.Violations) > 0 {
			break
		} // 非法完成结果不能凭 SKU 名称刷高需求覆盖率。
		for _, id := range r.AnyOfSKUs {
			if selected[id] {
				o.RequirementsMet++
				break
			}
		}
	}
	for _, call := range result.ToolsUsed {
		if strings.HasPrefix(call.Name, "primary.") && !call.Success {
			o.Fallback = true
		}
	}
	o.OutcomeMatched = e.Status == "completed" && o.Fallback == e.Fallback
	o.TaskSuccess = e.Feasibility == "satisfiable" && o.OutcomeMatched && len(o.Violations) == 0 && len(result.Items) > 0 && acceptable && o.RequirementsMet == o.RequirementsTotal
	return o
}

func Summarize(cases []CaseResult) Summary {
	s := Summary{Requests: len(cases), ErrorCounts: map[string]int{}}
	var outcomes, tasks, feasibleCases, met, needs, hits, relevant, facts, fallbacks, replays, replayOK, persisted int
	latencies := make([]float64, 0, len(cases))
	for _, c := range cases {
		if c.Status == "completed" {
			s.Completed++
			if c.FactConsistent {
				facts++
			}
		}
		if len(c.Violations) > 0 {
			s.HardViolationRequests++
		}
		if c.OutcomeMatched {
			outcomes++
		}
		if c.Feasibility == "satisfiable" {
			feasibleCases++
			if c.TaskSuccess {
				tasks++
			}
		}
		met += c.RequirementsMet
		needs += c.RequirementsTotal
		hits += c.RelevantHits
		relevant += c.RelevantTotal
		if c.RelevantTotal == 0 {
			s.NoRelevantCases++
		}
		if c.Fallback {
			fallbacks++
		}
		if c.ReplayChecked {
			replays++
			if c.ReplayOK {
				replayOK++
			}
		}
		if c.PersistenceOK {
			persisted++
		}
		s.ProviderCalls += c.ProviderCalls
		s.FaultPrimaryCalls += c.FaultPrimaryCalls
		s.ModelCalls += c.ModelCalls
		if c.ErrorCode != "" {
			s.ErrorCounts[c.ErrorCode]++
		}
		latencies = append(latencies, c.Timing.RecommendationMS)
	}
	s.HardViolationRate = ratio(s.HardViolationRequests, s.Completed)
	s.OutcomeAccuracy = ratio(outcomes, s.Requests)
	s.SatisfiableSuccess = ratio(tasks, feasibleCases)
	s.RequirementCoverage = ratio(met, needs)
	s.RecallAtK = ratio(hits, relevant)
	s.FactConsistency = ratio(facts, s.Completed)
	s.FallbackRate = ratio(fallbacks, s.Requests)
	s.ReplaySuccess = ratio(replayOK, replays)
	s.PersistenceSuccess = ratio(persisted, s.Requests)
	s.LatencyP50MS = percentile(latencies, 0.50)
	s.LatencyP95MS = percentile(latencies, 0.95)
	// 门禁只验证安全/终态/幂等，不通过放宽质量阈值隐藏推荐缺口。
	s.GatePassed = s.Requests > 0 && s.HardViolationRequests == 0 && outcomes == s.Requests && persisted == s.Requests && replays == s.Completed && replayOK == replays
	return s
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[int(math.Ceil(p*float64(len(sorted))))-1]
}
