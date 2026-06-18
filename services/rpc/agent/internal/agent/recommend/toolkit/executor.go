package toolkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

const (
	ToolSearchProducts = "search_products"
	ToolSelectBundle   = "select_bundle"
)

type Executor struct {
	provider *candidateStore
	selector *selector.BundleSelector
}

type SearchProductsArgs struct {
	Query       string   `json:"query"`
	Keywords    []string `json:"keywords"`
	BudgetCents int64    `json:"budget_cents"`
	MaxItems    int32    `json:"max_items"`
}

type SearchProductsResult struct {
	Products []tools.ProductCandidate `json:"products"`
	Count    int                      `json:"count"`
	Source   string                   `json:"source"`
}

type SelectBundleArgs struct {
	CandidateIDs []string `json:"candidate_ids"`
	BudgetCents  int64    `json:"budget_cents"`
	MaxItems     int32    `json:"max_items"`
}

type SelectBundleResult struct {
	Items           []agentcore.BundleItem `json:"items"`
	TotalPriceCents int64                  `json:"total_price_cents"`
}

type Result struct {
	Name   string          `json:"name"`
	JSON   json.RawMessage `json:"json"`
	Result any             `json:"-"`
}

func NewExecutor(provider tools.ProductProvider, selector *selector.BundleSelector) *Executor {
	return &Executor{
		provider: newCandidateStore(provider),
		selector: selector,
	}
}

func (e *Executor) Execute(ctx context.Context, name string, rawArgs json.RawMessage) (*Result, error) {
	switch name {
	case ToolSearchProducts:
		return e.searchProducts(ctx, rawArgs)
	case ToolSelectBundle:
		return e.selectBundle(ctx, rawArgs)
	default:
		return nil, fmt.Errorf("unknown recommend tool %q", name)
	}
}

func (e *Executor) searchProducts(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	var args SearchProductsArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.MaxItems <= 0 {
		args.MaxItems = 3
	}

	products, err := e.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       args.Query,
		Keywords:    args.Keywords,
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	if err != nil {
		return nil, err
	}

	result := SearchProductsResult{
		Products: products,
		Count:    len(products),
		Source:   e.provider.Name(),
	}
	return newResult(ToolSearchProducts, result)
}

func (e *Executor) selectBundle(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	_ = ctx

	var args SelectBundleArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.MaxItems <= 0 {
		args.MaxItems = 3
	}

	candidates := e.provider.Filter(args.CandidateIDs)
	if len(candidates) == 0 {
		return nil, errors.New("no product candidates available for select_bundle")
	}

	items, total := e.selector.Select(candidates, agentcore.Intent{
		BudgetCents: args.BudgetCents,
		MaxItems:    args.MaxItems,
	})
	return newResult(ToolSelectBundle, SelectBundleResult{
		Items:           items,
		TotalPriceCents: total,
	})
}

func decodeArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode recommend tool args: %w", err)
	}
	return nil
}

func newResult(name string, value any) (*Result, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s result: %w", name, err)
	}
	return &Result{
		Name:   name,
		JSON:   data,
		Result: value,
	}, nil
}
