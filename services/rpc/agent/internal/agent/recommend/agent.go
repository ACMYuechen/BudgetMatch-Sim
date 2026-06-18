package recommend

import (
	"context"
	"fmt"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/mcp"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

const AgentName = "recommend_agent"

type Agent struct {
	planner  *Planner
	provider tools.ProductProvider
	selector *selector.BundleSelector
	mcpCfg   mcp.Config
}

func NewAgent(provider tools.ProductProvider, selector *selector.BundleSelector, mcpCfg ...mcp.Config) *Agent {
	var cfg mcp.Config
	if len(mcpCfg) > 0 {
		cfg = mcpCfg[0]
	}
	return &Agent{
		planner:  NewPlanner(),
		provider: provider,
		selector: selector,
		mcpCfg:   cfg,
	}
}

func (a *Agent) Name() string {
	return AgentName
}

func (a *Agent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	intent := a.planner.Parse(input)
	candidates, err := a.provider.SearchProducts(ctx, tools.SearchProductsReq{
		Query:       input.Query,
		Keywords:    intent.Keywords,
		BudgetCents: intent.BudgetCents,
		MaxItems:    intent.MaxItems,
	})
	if err != nil {
		return nil, err
	}

	items, total := a.selector.Select(candidates, intent)
	toolsUsed := []agentcore.ToolCall{
		{Name: a.provider.Name(), Success: true, Detail: fmt.Sprintf("loaded %d mock candidates", len(candidates))},
	}
	toolsUsed = append(toolsUsed, a.probeMCP(ctx)...)

	return &agentcore.Result{
		Intent:          intent,
		Items:           items,
		TotalPriceCents: total,
		Summary:         summary(len(items), total, intent.BudgetCents),
		ToolsUsed:       toolsUsed,
	}, nil
}

func (a *Agent) probeMCP(ctx context.Context) []agentcore.ToolCall {
	if !a.mcpCfg.Ready() {
		return nil
	}

	probe, err := mcp.Probe(ctx, a.mcpCfg)
	if err != nil {
		return []agentcore.ToolCall{
			{Name: "mcp.demo", Success: false, Detail: err.Error()},
		}
	}

	name := "mcp.demo"
	if probe.ToolName != "" {
		name = "mcp." + probe.ToolName
	}
	detail := fmt.Sprintf("connected %d tools", probe.ToolCount)
	if probe.Message != "" {
		detail = detail + "; " + probe.Message
	}
	return []agentcore.ToolCall{
		{Name: name, Success: true, Detail: detail},
	}
}

func summary(count int, total, budget int64) string {
	if count == 0 {
		return "No bundle was found within the current budget."
	}
	if budget <= 0 {
		return fmt.Sprintf("Selected %d items with total price %.2f.", count, float64(total)/100)
	}
	return fmt.Sprintf("Selected %d items with total price %.2f, within budget %.2f.", count, float64(total)/100, float64(budget)/100)
}
