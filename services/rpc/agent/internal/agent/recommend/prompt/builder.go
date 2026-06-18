package prompt

import (
	"encoding/json"
	"fmt"
	"strings"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend/toolkit"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
)

const systemPrompt = `You are recommend_agent for BudgetMatch-Sim.
Your job is to understand the user's shopping goal, compare available products, and recommend a small MVP product bundle.

Rules:
- Stay within the user's budget when a budget is available.
- Prefer in-stock products with strong value, relevant category, and clear practical reasons.
- Do not invent products, prices, stock, tools, or discounts.
- If the product list is insufficient, say what is missing and keep the recommendation conservative.
- Return concise Chinese output for end users unless the user explicitly asks for another language.

Expected result:
- Summarize the user's intent.
- Select 1 to max_items products.
- Explain why each product belongs in the bundle.
- Mention total price in cents and yuan.
- Mention which tools or data sources were used.`

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type FunctionTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type BuildInput struct {
	Query           string
	Intent          agentcore.Intent
	Candidates      []tools.ProductCandidate
	SelectedItems   []agentcore.BundleItem
	TotalPriceCents int64
	ToolsUsed       []agentcore.ToolCall
}

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(input BuildInput) []Message {
	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: buildUserPrompt(input)},
	}
}

func (b *Builder) FunctionTools() []FunctionTool {
	return []FunctionTool{
		{
			Type:        "function",
			Name:        toolkit.ToolSearchProducts,
			Description: "Search product candidates by query, keywords, budget, and item limit.",
			Strict:      true,
			Parameters: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"query", "keywords", "budget_cents", "max_items"},
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Original user shopping request.",
					},
					"keywords": map[string]any{
						"type":        "array",
						"description": "Parsed shopping keywords.",
						"items": map[string]any{
							"type": "string",
						},
					},
					"budget_cents": map[string]any{
						"type":        "integer",
						"description": "Maximum budget in cents. Use 0 when unknown.",
					},
					"max_items": map[string]any{
						"type":        "integer",
						"description": "Maximum number of bundle items.",
					},
				},
			},
		},
		{
			Type:        "function",
			Name:        toolkit.ToolSelectBundle,
			Description: "Select an MVP product bundle from candidate product IDs.",
			Strict:      true,
			Parameters: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"candidate_ids", "budget_cents", "max_items"},
				"properties": map[string]any{
					"candidate_ids": map[string]any{
						"type":        "array",
						"description": "Product candidate IDs to compare.",
						"items": map[string]any{
							"type": "string",
						},
					},
					"budget_cents": map[string]any{
						"type":        "integer",
						"description": "Maximum budget in cents. Use 0 when unknown.",
					},
					"max_items": map[string]any{
						"type":        "integer",
						"description": "Maximum number of bundle items.",
					},
				},
			},
		},
	}
}

func buildUserPrompt(input BuildInput) string {
	var out strings.Builder
	out.WriteString("User request:\n")
	out.WriteString(input.Query)
	out.WriteString("\n\nParsed intent:\n")
	out.WriteString(mustJSON(map[string]any{
		"budget_cents": input.Intent.BudgetCents,
		"max_items":    input.Intent.MaxItems,
		"keywords":     input.Intent.Keywords,
		"preferences":  input.Intent.Preferences,
	}))

	out.WriteString("\n\nAvailable product candidates:\n")
	out.WriteString(mustJSON(input.Candidates))

	out.WriteString("\n\nCurrent deterministic bundle draft:\n")
	out.WriteString(mustJSON(map[string]any{
		"items":             input.SelectedItems,
		"total_price_cents": input.TotalPriceCents,
		"total_price_yuan":  fmt.Sprintf("%.2f", float64(input.TotalPriceCents)/100),
	}))

	out.WriteString("\n\nTools already used:\n")
	out.WriteString(mustJSON(input.ToolsUsed))

	out.WriteString("\n\nTask:\n")
	out.WriteString("Review the deterministic draft and produce the final MVP bundle recommendation. ")
	out.WriteString("Keep the answer grounded in the provided products and tool results.")
	return out.String()
}

func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
