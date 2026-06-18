package toolkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

const (
	ToolSearchProducts = "search_products"
	ToolSelectBundle   = "select_bundle"
	ToolMCPCallTool    = "mcp_call_tool"
)

type Executor struct {
	provider *candidateStore
	selector *selector.BundleSelector
	mcpCfg   mcp.Config
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

type MCPCallToolArgs struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type MCPCallToolResult struct {
	Name    string            `json:"name"`
	Content []mcp.ToolContent `json:"content"`
	IsError bool              `json:"is_error"`
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

func (e *Executor) WithMCP(c mcp.Config) *Executor {
	e.mcpCfg = c
	return e
}

func (e *Executor) Execute(ctx context.Context, name string, rawArgs json.RawMessage) (*Result, error) {
	switch name {
	case ToolSearchProducts:
		return e.searchProducts(ctx, rawArgs)
	case ToolSelectBundle:
		return e.selectBundle(ctx, rawArgs)
	case ToolMCPCallTool:
		return e.callMCPTool(ctx, rawArgs)
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

func (e *Executor) callMCPTool(ctx context.Context, rawArgs json.RawMessage) (*Result, error) {
	if !e.mcpCfg.Ready() {
		return nil, errors.New("mcp is not enabled")
	}

	var args MCPCallToolArgs
	if err := decodeArgs(rawArgs, &args); err != nil {
		return nil, err
	}
	if args.Name == "" {
		return nil, errors.New("mcp tool name is required")
	}
	if args.Arguments == nil {
		args.Arguments = map[string]any{}
	}

	client := mcp.NewClient(e.mcpCfg)
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	defer client.Close()

	toolResult, err := client.CallTool(ctx, args.Name, args.Arguments)
	if err != nil {
		return nil, err
	}

	return newResult(ToolMCPCallTool, MCPCallToolResult{
		Name:    args.Name,
		Content: toolResult.Content,
		IsError: toolResult.IsError,
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
