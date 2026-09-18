package recommend

import (
	"context"
	"fmt"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResultFinalizer runs once AFTER orchestration/fallback and BEFORE persistence.
// It never runs on idempotent replay. A failure cannot trigger another Agent.
type ResultFinalizer interface {
	Finalize(context.Context, *agentcore.Result) error
}

type CandidateFinalizer struct{ verifier tools.CandidateVerifier }

func NewCandidateFinalizer(verifier tools.CandidateVerifier) *CandidateFinalizer {
	return &CandidateFinalizer{verifier: verifier}
}

func (f *CandidateFinalizer) Finalize(ctx context.Context, result *agentcore.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f == nil || f.verifier == nil {
		return status.Error(codes.Unavailable, "candidate verifier unavailable")
	}
	limits, pool, err := selectionPool(result)
	if err != nil {
		return err
	}
	sel := selector.NewBundleSelector()
	// Selected IDs first, then baseline-ranked alternatives within the same scope.
	shortlist := make([]agentcore.ProductCandidate, 0, candidatecontract.MaxCandidates)
	byID := make(map[string]agentcore.ProductCandidate, len(pool))
	for _, candidate := range pool {
		byID[candidate.Id] = candidate
	}
	seen := make(map[string]bool)
	add := func(candidate agentcore.ProductCandidate) {
		if !seen[candidate.Id] && len(shortlist) < candidatecontract.MaxCandidates {
			seen[candidate.Id] = true
			shortlist = append(shortlist, candidate)
		}
	}
	for _, item := range result.Items {
		add(byID[item.Id])
	}
	for _, candidate := range sel.Rank(pool, limits.BudgetCents) {
		add(candidate)
	}
	if len(shortlist) == 0 {
		result.Candidates = nil
		result.Summary = agentcore.BundleSummary(0, 0, result.Intent.BudgetCents)
		return nil // no candidate to certify; do not claim a successful live check
	}
	batch, err := f.verifier.Verify(ctx, shortlist)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if batch.CheckedAtUnixMs <= 0 {
		return status.Error(codes.Unavailable, "missing candidate check evidence")
	}
	returned := make(map[string]bool)
	for _, candidate := range batch.Candidates {
		original := byID[candidate.Id]
		if !seen[candidate.Id] || returned[candidate.Id] ||
			candidate.Evidence.ProductID != original.Evidence.ProductID ||
			candidate.Evidence.Source != original.Evidence.Source ||
			(candidate.Evidence.Source != agentcore.RetrievalMallKeyword && candidate.Evidence.Source != agentcore.RetrievalMallVector) ||
			candidate.Evidence.State != agentcore.VerificationChecked ||
			candidate.Evidence.VerifiedAtUnixMs != batch.CheckedAtUnixMs {
			return status.Error(codes.Unavailable, "invalid candidate check evidence")
		}
		returned[candidate.Id] = true
	}
	result.Candidates = batch.Candidates
	result.Items, result.TotalPriceCents = sel.Select(batch.Candidates,
		agentcore.Intent{BudgetCents: limits.BudgetCents, MaxItems: limits.MaxItems})
	if err := limits.ValidateResult(result); err != nil {
		return err
	}
	result.Summary = agentcore.BundleSummary(len(result.Items), result.TotalPriceCents, result.Intent.BudgetCents) +
		fmt.Sprintf(" 商品状态核验于 %s；仅代表核验时点，下单前请重新确认价格和库存。",
			time.UnixMilli(batch.CheckedAtUnixMs).UTC().Format(time.RFC3339))
	result.ToolsUsed = append(result.ToolsUsed, agentcore.ToolCall{Name: "candidate.verify", Success: true, Detail: "status=ok"})
	return nil
}

func selectionPool(result *agentcore.Result) (agentcore.Constraints, []agentcore.ProductCandidate, error) {
	if result == nil {
		return agentcore.Constraints{}, nil, agentcore.ErrUnsafeResult
	}
	limits, err := agentcore.NewConstraints(result.Intent)
	if err != nil {
		return limits, nil, err
	}
	if err := limits.ValidateResult(result); err != nil {
		return limits, nil, err
	}
	pool := agentcore.NormalizeCandidates(result.Candidates)
	if scope := result.Selection; scope != nil {
		var adjusted bool
		limits, adjusted, err = limits.Restrict(scope.Limits.BudgetCents, scope.Limits.MaxItems)
		if err != nil || adjusted || scope.Limits.BudgetCents <= 0 || scope.Limits.MaxItems <= 0 {
			return limits, nil, fmt.Errorf("%w: invalid selection limits", agentcore.ErrUnsafeResult)
		}
		allowed := make(map[string]bool, len(scope.CandidateIDs))
		for _, id := range scope.CandidateIDs {
			allowed[id] = true
		}
		filtered := make([]agentcore.ProductCandidate, 0, len(pool))
		for _, candidate := range pool {
			if allowed[candidate.Id] {
				filtered = append(filtered, candidate)
			}
		}
		pool = filtered
	}
	// Also reject a selection that already violated its narrower internal scope.
	copyResult := *result
	copyResult.Candidates = pool
	if err := limits.ValidateResult(&copyResult); err != nil {
		return limits, nil, err
	}
	return limits, pool, nil
}

type DemoFinalizer struct{}

func (DemoFinalizer) Finalize(ctx context.Context, result *agentcore.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, pool, err := selectionPool(result)
	if err != nil {
		return err
	}
	for _, candidate := range pool {
		if candidate.Evidence.Source != agentcore.RetrievalDemo || candidate.Evidence.State != agentcore.VerificationDemo {
			return fmt.Errorf("%w: non-demo evidence in demo mode", agentcore.ErrUnsafeResult)
		}
	}
	result.Summary = "【演示数据，非实时商城库存】" + agentcore.BundleSummary(len(result.Items), result.TotalPriceCents, result.Intent.BudgetCents)
	return nil
}
