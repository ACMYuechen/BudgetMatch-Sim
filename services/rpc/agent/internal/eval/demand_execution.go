package eval

import (
	"context"
	"errors"
	"slices"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demandexec"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DemandExecutionOutcome struct {
	ID string `json:"id"`
	DemandAssessment
	FaultStage        string       `json:"fault_stage"`
	Fault             string       `json:"fault"`
	Outcome           string       `json:"outcome"`
	Expected          string       `json:"expected"`
	OutcomeMatched    bool         `json:"outcome_matched"`
	FinalOracle       DemandOracle `json:"final_retrieved_snapshot_oracle"`
	ProviderCalls     int          `json:"provider_calls"`
	CheckCalls        int          `json:"check_calls"`
	ExpectedCalls     int          `json:"expected_check_calls"`
	CheckIDs          [][]string   `json:"check_ids"`
	InitialExpansions int          `json:"initial_expansions"`
	FinalExpansions   int          `json:"final_expansions"`
	SearchLimited     bool         `json:"search_limited"`
	GateProblems      []string     `json:"gate_problems"`
	ReplayMS          float64      `json:"replay_ms"`
}

type DemandExecutionSummary struct {
	Cases             int     `json:"cases"`
	SnapshotCases     int     `json:"snapshot_cases"`
	FaultCases        int     `json:"fault_cases"`
	OutcomeAccuracy   Ratio   `json:"outcome_accuracy"`
	FaultClosed       Ratio   `json:"fault_closed"`
	HardViolationRate Ratio   `json:"nonempty_hard_violation_rate"`
	SnapshotSuccess   Ratio   `json:"snapshot_satisfiable_success"`
	ProviderCalls     int     `json:"provider_calls"`
	CheckCalls        int     `json:"check_calls"`
	SearchExpansions  int     `json:"search_expansions"`
	ReplayP50MS       float64 `json:"snapshot_replay_p50_ms"`
	ReplayP95MS       float64 `json:"snapshot_replay_p95_ms"`
}

func runDemandExecution(ctx context.Context, c DemandExecutionCase, base DemandSelectionCase, products []DemandProduct) (DemandExecutionOutcome, error) {
	if err := ctx.Err(); err != nil {
		return DemandExecutionOutcome{}, err
	}
	// The recorder receives facts/fault injection only, NEVER expected outcomes.
	backend := &demandReplay{initial: slices.Clone(products), final: slices.Clone(products),
		stage: c.FaultStage, fault: c.Fault, checkIDs: [][]string{}, problems: []string{}}
	for i, p := range backend.final {
		for _, update := range c.Updates {
			if update.ID == p.ID {
				backend.final[i] = update
				break
			}
		}
	}
	// Execution has keyword-only evidence. Synthetic RRF is selector-only.
	for i := range backend.final {
		backend.final[i].KeywordRank, backend.final[i].VectorRank = 0, 0
	}
	oracle, err := demandOracle(ctx, base.State, backend.final)
	if err != nil {
		return DemandExecutionOutcome{}, err
	}
	executor, err := demandexec.NewMall(backend, backend)
	if err != nil {
		return DemandExecutionOutcome{}, err
	}
	start := time.Now()
	result, runErr := executor.Run(ctx, demandIntent(base.State))
	elapsed := time.Since(start)
	if err := ctx.Err(); err != nil {
		return DemandExecutionOutcome{}, err
	}
	out := DemandExecutionOutcome{ID: c.ID, FaultStage: c.FaultStage, Fault: c.Fault, Expected: c.Expected,
		FinalOracle: oracle, ProviderCalls: backend.reads, CheckCalls: backend.checks, ExpectedCalls: c.ExpectedCalls,
		CheckIDs: backend.checkIDs, GateProblems: slices.Clone(backend.problems), ReplayMS: float64(elapsed) / float64(time.Millisecond)}
	var items []agent.BundleItem
	var total int64
	if runErr != nil {
		out.Outcome = demandError(runErr)
		if result != nil {
			out.GateProblems = append(out.GateProblems, "result_with_error")
		}
	} else if result == nil {
		out.Outcome = "nil_result"
		out.GateProblems = append(out.GateProblems, "nil_result")
	} else {
		out.Outcome, items, total = result.Status, result.Items, result.TotalPriceCents
		if result.Intent.BudgetCents != base.State.BudgetCents || result.Intent.MaxItems != base.State.MaxItems {
			out.GateProblems = append(out.GateProblems, "intent_changed")
		}
		if result.Execution == nil {
			out.GateProblems = append(out.GateProblems, "missing_execution_metadata")
		} else {
			e := result.Execution
			out.InitialExpansions, out.FinalExpansions, out.SearchLimited = int(e.InitialExpansions), int(e.FinalExpansions), e.SearchLimited
			if !e.SearchLimited || e.Scope != "mall_checked_snapshot_only" || (len(items) > 0 && (backend.checks != 2 || e.SnapshotCheckedAtUnixMs <= 0)) {
				out.GateProblems = append(out.GateProblems, "snapshot_scope")
			}
			if e.InitialExpansions > 8192 || e.FinalExpansions > 8192 || e.CandidateWindow > 32 {
				out.GateProblems = append(out.GateProblems, "search_bounds")
			}
			if e.InitialStopReason == "time_budget" || e.FinalStopReason == "time_budget" {
				out.GateProblems = append(out.GateProblems, "time_budget_inconclusive")
			}
		}
	}
	out.DemandAssessment = assessDemand(base.State, backend.final, items, total)
	out.OutcomeMatched = out.Outcome == c.Expected
	if len(out.Violations) > 0 || out.Outcome == "complete" && !out.TaskSuccess || out.Outcome != "complete" && len(items) > 0 {
		out.GateProblems = append(out.GateProblems, "unsafe_execution")
	}
	if backend.reads != 1 || backend.checks != c.ExpectedCalls || backend.checks > 2 {
		out.GateProblems = append(out.GateProblems, "call_bounds_or_expectation")
	}
	if c.Fault != "" && (runErr == nil || result != nil) {
		out.GateProblems = append(out.GateProblems, "fault_not_closed")
	}
	return out, nil
}

func demandError(err error) string {
	switch {
	case errors.Is(err, agent.ErrUnsafeResult):
		return "unsafe_result"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	}
	switch status.Code(err) {
	case codes.Unavailable:
		return "unavailable"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Canceled:
		return "canceled"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "unexpected_error"
	}
}

// In-memory adapter, NOT a Mall server/client transport or database simulator.
// Calls count attempted interface invocations, including failures, not RPCs.
type demandReplay struct {
	initial, final []DemandProduct
	stage, fault   string
	reads, checks  int
	checkIDs       [][]string
	problems       []string
}

func (r *demandReplay) SearchRankedProducts(ctx context.Context, _ tools.SearchProductsReq, limit int) ([]agent.ProductCandidate, error) {
	r.reads++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit != 32 {
		r.problems = append(r.problems, "provider_limit")
	}
	if _, ok := ctx.Deadline(); !ok {
		r.problems = append(r.problems, "missing_execution_deadline")
	}
	if r.stage == "search" {
		if err := r.injectError(); err != nil {
			return nil, err
		}
	}
	out := demandCandidates(r.initial)
	for i := range out {
		// Stale display fields have no authority. Verification must overwrite them.
		out[i].Name, out[i].Category, out[i].PriceCents = "stale", "untrusted", 1
		out[i].Evidence = agent.CandidateEvidence{Source: agent.RetrievalMallKeyword, ProductID: "p-" + out[i].Id}
	}
	return out, nil
}

func (r *demandReplay) CheckProductCandidates(ctx context.Context, req *pb.CheckProductCandidatesReq, _ ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	r.checks++
	r.checkIDs = append(r.checkIDs, slices.Clone(req.SkuIds))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !req.IncludeDemandCategory || !candidatecontract.ValidIDs(req.SkuIds) {
		r.problems = append(r.problems, "check_contract")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > candidatecontract.MaxDuration {
		r.problems = append(r.problems, "check_deadline")
	}
	stage, products := "initial_check", r.initial
	if r.checks > 1 {
		stage, products = "final_check", r.final
	}
	if stage == r.stage {
		if err := r.injectError(); err != nil {
			return nil, err
		}
	}
	resp := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli(), DemandCategoryContract: candidatecontract.DemandCategoryContract}
	for _, id := range req.SkuIds {
		var product DemandProduct
		for _, p := range products {
			if p.ID == id {
				product = p
				break
			}
		}
		if product.ID == "" {
			r.problems = append(r.problems, "check_outside_snapshot")
			continue
		}
		if r.checks == 2 && !slices.Contains(r.checkIDs[0], id) {
			r.problems = append(r.problems, "recheck_scope_widened")
		}
		item := &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE}
		if product.Stock > 0 {
			item.State = pb.CandidateState_CANDIDATE_STATE_ACTIVE
			item.Facts = &pb.CandidateFacts{SkuId: id, ProductId: "p-" + id, ProductName: "fixture " + id,
				Price: product.PriceCents, Stock: product.Stock, Sold: product.Sold,
				DemandCategory: &pb.DemandCategoryFact{Code: string(product.Category), TaxonomyVersion: candidatecontract.DemandTaxonomyVersion, Revision: product.Revision}}
		}
		resp.Results = append(resp.Results, item)
	}
	if stage == r.stage {
		switch r.fault {
		case "bad_contract":
			resp.DemandCategoryContract = ""
		case "missing_fact":
			if len(resp.Results) > 0 {
				resp.Results = resp.Results[1:]
			}
		case "revision_rollback":
			for _, item := range resp.Results {
				if item.Facts != nil {
					item.Facts.DemandCategory.Revision--
					break
				}
			}
		}
	}
	return resp, nil
}

func (r *demandReplay) injectError() error {
	switch r.fault {
	case "unavailable":
		return status.Error(codes.Unavailable, "injected")
	case "permission_denied":
		return status.Error(codes.PermissionDenied, "injected")
	case "deadline_exceeded":
		return status.Error(codes.DeadlineExceeded, "injected")
	case "canceled":
		return status.Error(codes.Canceled, "injected")
	default:
		return nil
	}
}

func summarizeDemandExecution(cases []DemandExecutionOutcome) DemandExecutionSummary {
	s := DemandExecutionSummary{Cases: len(cases)}
	var matched, closed, nonempty, violations, feasible, success int
	var durations []float64
	for _, c := range cases {
		if c.OutcomeMatched {
			matched++
		}
		s.ProviderCalls += c.ProviderCalls
		s.CheckCalls += c.CheckCalls
		s.SearchExpansions += c.InitialExpansions + c.FinalExpansions
		if len(c.SelectedIDs) > 0 {
			nonempty++
			if len(c.Violations) > 0 {
				violations++
			}
		}
		if c.Fault != "" {
			s.FaultCases++
			if c.OutcomeMatched && len(c.GateProblems) == 0 && len(c.SelectedIDs) == 0 {
				closed++
			}
		} else {
			s.SnapshotCases++
			if c.FinalOracle.Feasible {
				feasible++
				if c.TaskSuccess {
					success++
				}
			}
			durations = append(durations, c.ReplayMS)
		}
	}
	s.OutcomeAccuracy, s.FaultClosed = ratio(matched, len(cases)), ratio(closed, s.FaultCases)
	s.HardViolationRate, s.SnapshotSuccess = ratio(violations, nonempty), ratio(success, feasible)
	s.ReplayP50MS, s.ReplayP95MS = percentile(durations, .5), percentile(durations, .95)
	return s
}
