// Package eino 提供基于 Eino 框架的 LLM 模型封装、Prompt 构建、工具定义与 ReAct Agent 运行器。
package eino

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// systemPrompt 是 ReAct Agent 的系统提示词，定义了推荐代理的行为规则与输出格式。
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

// BuildMessages 根据 RunInput 构建 Eino 对话消息列表，包含系统提示与用户输入。
func BuildMessages(input RunInput) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(buildUserPrompt(input)),
	}
}

// buildUserPrompt 将 RunInput 中的查询、意图、已选商品及已用工具等信息拼接为结构化用户提示。
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

// mustJSON 将任意值序列化为 JSON 字符串；若失败则返回其 fmt.Sprintf 格式。
func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
