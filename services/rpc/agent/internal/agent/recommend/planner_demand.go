package recommend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
)

// A distinct method and canonical patch participate in turn identity. Raw query
// and numeric inputs are compared separately, preserving legacy retry semantics.
func decodeDemandRequest(raw string) (demand.Patch, string, error) {
	if raw == "" {
		raw = `{"schema_version":1}`
	}
	patch, err := demand.DecodePatch(strings.NewReader(raw))
	if err != nil || patch.BudgetCents != nil || patch.MaxItems != nil {
		return demand.Patch{}, "", agent.ErrInvalidInput
	}
	for _, change := range []*demand.Change[demand.Category]{patch.Required, patch.Optional, patch.Excluded} {
		if change != nil {
			slices.Sort(change.Values)
		}
	}
	if patch.Preferences != nil {
		slices.Sort(patch.Preferences.Values)
	}
	canonical, err := json.Marshal(patch)
	if err != nil {
		return demand.Patch{}, "", agent.ErrInvalidInput
	}
	sum := sha256.Sum256(append([]byte("plan-demand-v1:"), canonical...))
	return patch, hex.EncodeToString(sum[:]), nil
}

// ResolveDemand plans only: ready means a valid intent, never a fulfilled order.
// Numeric limits retain the legacy precedence. Category/preference changes come
// exclusively from an explicit patch, not a model or repeated historical text.
func (p *Planner) ResolveDemand(input agent.Input, history []string, patch demand.Patch) (*agent.Result, error) {
	legacy := input
	if input.PriorIntent != nil {
		prior := cloneIntent(*input.PriorIntent)
		prior.Demand = nil
		legacy.PriorIntent = &prior
	}
	baseInput := legacy
	baseInput.Query, baseInput.BudgetCents, baseInput.MaxItems = "", 0, 0
	base, err := p.Resolve(baseInput, history)
	if err != nil {
		return nil, err
	}
	if input.PriorIntent != nil && input.PriorIntent.Demand != nil {
		// Stored structured state must validate as-is; defaults cannot repair it.
		base = cloneIntent(*input.PriorIntent)
	}
	state := demand.State{SchemaVersion: demand.SchemaVersion, BudgetCents: base.BudgetCents, MaxItems: base.MaxItems}
	if view := base.Demand; view != nil {
		state.SchemaVersion = view.SchemaVersion
		state.Required = toTerms[demand.Category](view.Required)
		state.Optional = toTerms[demand.Category](view.Optional)
		state.Excluded = toTerms[demand.Category](view.Excluded)
		state.Preferences = toTerms[demand.Preference](base.Preferences)
		if state.Validate() != nil {
			return nil, agent.ErrUnsafeResult
		}
	} else {
		var supported bool
		state.Preferences, supported = migratePreferences(base.Preferences)
		if !supported {
			if patch.Preferences == nil || patch.Preferences.Operation != demand.Replace {
				return demandResult(base, "needs_clarification", []agent.DemandConflict{{Code: "legacy_preference_requires_replace"}}), nil
			}
			state.Preferences = nil
		}
	}
	current, err := p.Resolve(legacy, history)
	if err != nil {
		return nil, err
	}
	patch.BudgetCents, patch.MaxItems = &current.BudgetCents, &current.MaxItems
	transition, err := demand.Apply(state, patch)
	if err != nil {
		return nil, agent.ErrInvalidInput
	}
	if transition.Status == "needs_clarification" {
		issues := make([]agent.DemandConflict, 0, len(transition.Conflicts))
		for _, issue := range transition.Conflicts {
			issues = append(issues, agent.DemandConflict{Code: issue.Code, Category: string(issue.Category)})
		}
		return demandResult(base, "needs_clarification", issues), nil
	}
	current.Demand = &agent.DemandState{
		SchemaVersion: transition.State.SchemaVersion,
		Required:      fromTerms(transition.State.Required), Optional: fromTerms(transition.State.Optional),
		Excluded: fromTerms(transition.State.Excluded),
	}
	current.Preferences = fromTerms(transition.State.Preferences)
	return demandResult(current, "intent_ready", nil), nil
}

func demandResult(intent agent.Intent, status string, conflicts []agent.DemandConflict) *agent.Result {
	summary := "需求状态已保存；尚未执行商品检索或组合推荐。"
	if status == "needs_clarification" {
		summary = "需求存在冲突，请修改后重试；本轮变更未应用，未执行商品推荐。"
	}
	return &agent.Result{Intent: cloneIntent(intent), Status: status, DemandConflicts: conflicts,
		Items: []agent.BundleItem{}, ToolsUsed: []agent.ToolCall{}, Summary: summary}
}

func toTerms[T ~string](values []string) []T {
	out := make([]T, len(values))
	for i, value := range values {
		out[i] = T(value)
	}
	return out
}

func fromTerms[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func migratePreferences(values []string) ([]demand.Preference, bool) {
	aliases := map[string]demand.Preference{
		"value": "value", "cheap": "value", "性价比": "value", "便宜": "value",
		"portable": "portable", "便携": "portable", "battery_life": "battery_life",
		"battery life": "battery_life", "long battery": "battery_life", "续航": "battery_life",
		"lightweight": "lightweight", "slim": "lightweight", "轻薄": "lightweight",
		"performance": "performance", "性能": "performance", "durable": "durable",
		"durability": "durable", "耐用": "durable", "quiet": "quiet", "silent": "quiet", "静音": "quiet",
	}
	out := []demand.Preference{}
	for _, value := range values {
		canonical, ok := aliases[value]
		if !ok {
			return nil, false
		}
		if !slices.Contains(out, canonical) {
			out = append(out, canonical)
		}
	}
	return out, true
}
