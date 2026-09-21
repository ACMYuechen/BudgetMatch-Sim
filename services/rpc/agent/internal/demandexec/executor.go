// Package demandexec connects bounded selection to an explicit snapshot recheck
// and reselection. Demo and opt-in Mall execution have disjoint evidence scopes.
package demandexec

import (
	"context"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
)

const MaxExecutionTime = 3 * time.Second

// Batch must account for every requested SKU exactly once, either active or
// explicitly unavailable. Omission cannot turn an incomplete check into success.
type Batch struct {
	Candidates  []agent.ProductCandidate
	Unavailable []string
	CheckedAtMs int64
}

// Source is provider-authored, never model output. New accepts only demo evidence;
// NewMall installs the classified Mall source. Implementations must honor context.
type Source interface {
	Search(context.Context, agent.Intent) ([]agent.ProductCandidate, error)
	Recheck(context.Context, []agent.ProductCandidate) (Batch, error)
}

type Executor struct {
	source        Source
	catalog       *demand.Catalog
	search        *beam.Selector
	mall          bool
	mallRetrieval bool
}

func New(source Source, catalog *demand.Catalog, config beam.Config) (*Executor, error) {
	_, digest := catalog.Metadata()
	if source == nil || digest == "" || catalog.Scope() != beam.SnapshotScope {
		return nil, agent.ErrInvalidInput
	}
	if config.MaxCandidates == 0 {
		config.MaxCandidates = candidatecontract.MaxCandidates
	}
	if config.MaxCandidates > candidatecontract.MaxCandidates {
		return nil, beam.ErrConfig
	}
	search, err := beam.New(config)
	if err != nil {
		return nil, err
	}
	return &Executor{source: source, catalog: catalog, search: search}, nil
}

func cloneIntent(intent agent.Intent) agent.Intent {
	intent.Demand = agent.CloneDemand(intent.Demand)
	intent.Keywords = slices.Clone(intent.Keywords)
	intent.Preferences = slices.Clone(intent.Preferences)
	return intent
}

func stateFromIntent(intent agent.Intent) (demand.State, error) {
	if intent.Demand == nil || len(intent.Keywords) > agent.MaxKeywords || len(intent.Preferences) > demand.MaxTerms ||
		len(intent.Demand.Required) > demand.MaxTerms || len(intent.Demand.Optional) > demand.MaxTerms || len(intent.Demand.Excluded) > demand.MaxTerms {
		return demand.State{}, agent.ErrInvalidInput
	}
	for _, keyword := range intent.Keywords {
		if len(keyword) > 4*agent.MaxKeywordRunes || !utf8.ValidString(keyword) || utf8.RuneCountInString(keyword) > agent.MaxKeywordRunes {
			return demand.State{}, agent.ErrInvalidInput
		}
	}
	state := demand.State{SchemaVersion: intent.Demand.SchemaVersion, BudgetCents: intent.BudgetCents, MaxItems: intent.MaxItems}
	for _, c := range intent.Demand.Required {
		state.Required = append(state.Required, demand.Category(c))
	}
	for _, c := range intent.Demand.Optional {
		state.Optional = append(state.Optional, demand.Category(c))
	}
	for _, c := range intent.Demand.Excluded {
		state.Excluded = append(state.Excluded, demand.Category(c))
	}
	for _, p := range intent.Preferences {
		state.Preferences = append(state.Preferences, demand.Preference(p))
	}
	if state.Validate() != nil {
		return demand.State{}, agent.ErrUnsafeResult
	}
	return state, nil
}

// Run never calls the legacy Agent/model/fallback. Initial search selects the
// check scope; only the rechecked snapshots can produce the returned bundle.
func (e *Executor) Run(ctx context.Context, intent agent.Intent) (*agent.Result, error) {
	if e == nil || e.source == nil || e.search == nil {
		return nil, agent.ErrDemandNotExecutable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, MaxExecutionTime)
	defer cancel()
	state, err := stateFromIntent(intent)
	if err != nil {
		return nil, err
	}
	intent = cloneIntent(intent)
	candidates, err := e.source.Search(ctx, cloneIntent(intent))
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if err != nil {
		return nil, err
	}
	catalog, err := e.catalogFor(candidates)
	if err != nil {
		return nil, err
	}
	initial, err := e.search.Select(ctx, state, catalog, candidates)
	if err != nil {
		return nil, err
	}
	final := initial
	var checkedAt int64
	if len(initial.Window) > 0 {
		started := time.Now()
		checkCtx, cancelCheck := context.WithTimeout(ctx, candidatecontract.MaxDuration)
		batch, err := e.source.Recheck(checkCtx, cloneCandidates(initial.Window))
		checkErr := checkCtx.Err()
		cancelCheck()
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if checkErr != nil {
			return nil, checkErr
		}
		if err != nil {
			return nil, err
		}
		checked, err := e.validateBatch(initial.Window, batch, started)
		if err != nil {
			return nil, err
		}
		checkedAt = batch.CheckedAtMs
		catalog, err = e.catalogFor(checked)
		if err != nil {
			return nil, err
		}
		final, err = e.search.Select(ctx, state, catalog, checked)
		if err != nil {
			return nil, err
		}
	}
	result, err := e.result(intent, state, catalog, initial, final, checkedAt)
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	return result, err
}

func cloneCandidates(input []agent.ProductCandidate) []agent.ProductCandidate {
	out := slices.Clone(input)
	for i := range out {
		out[i].Tags = slices.Clone(out[i].Tags)
	}
	return out
}

func (e *Executor) validateBatch(window []agent.ProductCandidate, batch Batch, started time.Time) ([]agent.ProductCandidate, error) {
	if len(window) > candidatecontract.MaxCandidates || len(batch.Candidates)+len(batch.Unavailable) != len(window) ||
		batch.CheckedAtMs < started.Add(-30*time.Second).UnixMilli() || batch.CheckedAtMs > time.Now().Add(30*time.Second).UnixMilli() || !boundedBatch(batch) {
		return nil, agent.ErrUnsafeResult
	}
	allowed := make(map[string]agent.ProductCandidate, len(window))
	for _, c := range window {
		allowed[c.Id] = c
	}
	seen := map[string]bool{}
	out := make([]agent.ProductCandidate, 0, len(batch.Candidates))
	for _, c := range batch.Candidates {
		original, exists := allowed[c.Id]
		if !exists || seen[c.Id] || c.Evidence.ProductID != original.Evidence.ProductID ||
			c.Evidence.Source != original.Evidence.Source || !e.validEvidence(c) ||
			c.Evidence.VerifiedAtUnixMs != batch.CheckedAtMs || c.Evidence.Ranking != original.Evidence.Ranking ||
			c.PriceCents <= 0 || c.PriceCents > agent.MaxBudgetCents || c.Stock <= 0 || c.Sold < 0 {
			return nil, agent.ErrUnsafeResult
		}
		seen[c.Id] = true
		if e.mall {
			old, next := original.Evidence.DemandCategory, c.Evidence.DemandCategory
			if next.Revision > 0 && (next.Revision < old.Revision || next.Revision == old.Revision && next != old) {
				return nil, agent.ErrUnsafeResult
			}
			// Classification may change. The final selection binds a NEW catalog.
			c.Category, c.Source = c.Evidence.DemandCategory.Code, "mall"
			if c.Evidence.Source == agent.RetrievalMallVector {
				c.Source = "mall+rag"
			}
		} else {
			c.Category, c.Source = string(e.catalog.Classify(c).Category), "synthetic_demo"
		}
		c.Tags = slices.Clone(c.Tags)
		out = append(out, c)
	}
	for _, id := range batch.Unavailable {
		if _, exists := allowed[id]; !exists || seen[id] {
			return nil, agent.ErrUnsafeResult
		}
		seen[id] = true
	}
	return out, nil
}

func (e *Executor) validEvidence(c agent.ProductCandidate) bool {
	if e.mall {
		return (c.Evidence.Source == agent.RetrievalMallKeyword || e.mallRetrieval && c.Evidence.Source == agent.RetrievalMallVector) && c.Evidence.State == agent.VerificationChecked &&
			candidatecontract.ValidDemandCategoryFact(c.Evidence.DemandCategory.Code, c.Evidence.DemandCategory.TaxonomyVersion, c.Evidence.DemandCategory.Revision)
	}
	return c.Evidence.Source == agent.RetrievalDemo && c.Evidence.State == agent.VerificationDemo
}

// Bound returned payloads BEFORE cloning tags or hashing provider-supplied IDs.
// A count bound alone does not bound the memory held by a single snapshot.
func boundedBatch(batch Batch) bool {
	remaining := beam.MaxInputBytes
	consume := func(value string) bool {
		if len(value) > remaining {
			return false
		}
		remaining -= len(value)
		return true
	}
	for _, c := range batch.Candidates {
		if !candidatecontract.ValidID(c.Id) || len(c.Tags) > beam.MaxTagsPerSKU {
			return false
		}
		for _, value := range []string{c.Id, c.Name, c.Category, c.Source, c.Evidence.ProductID, c.Evidence.Ranking.Method,
			c.Evidence.DemandCategory.Code, c.Evidence.DemandCategory.TaxonomyVersion} {
			if !consume(value) {
				return false
			}
		}
		for _, tag := range c.Tags {
			if !consume(tag) {
				return false
			}
		}
	}
	for _, id := range batch.Unavailable {
		if !candidatecontract.ValidID(id) || !consume(id) {
			return false
		}
	}
	return true
}

func stringsOf[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

func (e *Executor) result(intent agent.Intent, state demand.State, catalog *demand.Catalog, initial, final beam.Result, checkedAt int64) (*agent.Result, error) {
	metadata, digest := catalog.Metadata()
	explanation := &agent.DemandExecution{Strategy: beam.StrategyVersion, Scope: catalog.Scope(),
		MappingVersion: metadata.Version, MappingSHA256: digest, SnapshotCheckedAtUnixMs: checkedAt,
		CoveredRequired: []string{}, CoveredOptional: []string{}, MissingRequired: stringsOf(state.Required),
		UnscoredPreferences: stringsOf(final.UnscoredPreferences), MissingRequiredInWindow: stringsOf(final.MissingRequiredInWindow),
		SearchLimited: e.mall || initial.Stats.SearchLimited || final.Stats.SearchLimited, CandidateWindow: int32(len(initial.Window)),
		InitialExpansions: int32(initial.Stats.Expansions), InitialStopReason: initial.Stats.StopReason}
	if checkedAt > 0 {
		explanation.FinalExpansions, explanation.FinalStopReason = int32(final.Stats.Expansions), final.Stats.StopReason
	}
	result := &agent.Result{Intent: cloneIntent(intent), Status: final.Status, Execution: explanation,
		Candidates: cloneCandidates(final.Selected), Items: []agent.BundleItem{}, ToolsUsed: []agent.ToolCall{}}
	if final.Status == beam.Complete {
		if checkedAt == 0 || len(final.Selected) == 0 {
			return nil, agent.ErrUnsafeResult
		}
		assessment, err := demand.AssessSelection(state, catalog, final.Selected)
		if err != nil || !assessment.ConstraintsSatisfied {
			return nil, agent.ErrUnsafeResult
		}
		explanation.CoveredRequired, explanation.CoveredOptional = stringsOf(assessment.CoveredRequired), stringsOf(assessment.CoveredOptional)
		explanation.MissingRequired = []string{}
		for i, c := range final.Selected {
			reason := fmt.Sprintf("演示分类：%s；仅基于快照事实与有界搜索。", c.Category)
			if e.mall {
				reason = fmt.Sprintf("Mall 分类：%s；已核验快照，不代表属性保证或库存预占。", c.Category)
			}
			result.Items = append(result.Items, agent.BundleItem{Id: c.Id, Name: c.Name, Category: c.Category, Source: c.Source,
				PriceCents: c.PriceCents, Stock: c.Stock, Score: final.Evidence[i].RankUtility, Reason: reason})
		}
		result.TotalPriceCents = assessment.TotalPriceCents
	} else if final.Status != beam.NoFeasibleBundle || len(final.Selected) != 0 {
		return nil, agent.ErrUnsafeResult
	}
	limits := agent.Constraints{BudgetCents: state.BudgetCents, MaxItems: state.MaxItems}
	if err := limits.ValidateResult(result); err != nil {
		return nil, err
	}
	prefix, searchTool, checkTool := "【演示数据，非实时商城库存】", "demand.demo_search", "demand.demo_recheck"
	if e.mall {
		prefix, searchTool, checkTool = "【商城快照，有界核验，非库存预占】", "demand.mall_search", "demand.mall_recheck"
	}
	result.Summary = prefix + agent.BundleSummary(len(result.Items), result.TotalPriceCents, state.BudgetCents)
	if final.Status == beam.NoFeasibleBundle {
		result.Summary = prefix + "在本次候选与搜索范围内未找到满足全部硬约束的组合，不代表全局无解。"
	}
	if len(explanation.UnscoredPreferences) > 0 {
		result.Summary += " 部分属性偏好缺少证据，未参与评分。"
	}
	result.ToolsUsed = append(result.ToolsUsed, agent.ToolCall{Name: searchTool, Success: true, Detail: "status=ok"})
	if checkedAt > 0 {
		result.ToolsUsed = append(result.ToolsUsed, agent.ToolCall{Name: checkTool, Success: true, Detail: "status=ok"})
	}
	return result, nil
}
