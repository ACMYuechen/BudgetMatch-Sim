package eino

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const systemPrompt = `You are recommend_agent for BudgetMatch-Sim.
Your job is to understand the user's shopping goal, compare available products, and recommend a small MVP product bundle.

Rules:
- Stay within the user's budget when a budget is available.
- Prefer in-stock products with strong value, relevant category, and clear practical reasons.
- Do not invent products, prices, stock, tools, or discounts.
- Use search_products before selecting a bundle, then use select_bundle with real candidate IDs.
- Use mcp_call_tool only when MCP is enabled and the user's goal benefits from an external MCP tool.
- If the product list is insufficient, say what is missing and keep the recommendation conservative.
- Return concise Chinese output for end users unless the user explicitly asks for another language.

Expected result:
- Summarize the user's intent.
- Select 1 to max_items products.
- Explain why each product belongs in the bundle.
- Mention total price in cents and yuan.
- Mention which tools or data sources were used.`

func BuildMessages(input RunInput) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(buildUserPrompt(input)),
	}
}

func buildUserPrompt(input RunInput) string {
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
	out.WriteString("Call search_products first, then call select_bundle. ")
	out.WriteString("Keep the answer grounded in real tool results.")
	return out.String()
}

func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
