package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"

	"github.com/cloudwego/eino/schema"
)

// systemPrompt 是 ReAct Agent 的系统提示词，约束推荐代理的行为与输出格式。
const systemPrompt = `You are recommend_agent for BudgetMatch-Sim.
Your job is to understand the user's shopping goal, compare available products, and recommend a small MVP product bundle.

Rules:
- Stay within the user's budget when a budget is available.
- Prefer in-stock products with strong value, relevant category, and clear practical reasons.
- Do not invent products, prices, stock, tools, or discounts.
- Use search_products before selecting a bundle, then use select_bundle with real candidate IDs.
- Use external MCP tools only when the user's goal clearly benefits from them.
- If the product list is insufficient, say what is missing and keep the recommendation conservative.
- Return concise Chinese output for end users unless the user explicitly asks for another language.

Expected result:
- Summarize the user's intent.
- Select 1 to max_items products.
- Explain why each product belongs in the bundle.
- Mention total price in cents and yuan.
- Mention which tools or data sources were used.`

// buildMessages 根据用户输入与解析意图构建 Eino 对话消息列表。
func buildMessages(input agentcore.Input, intent agentcore.Intent) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(buildUserPrompt(input, intent)),
	}
}

// buildUserPrompt 将用户请求与解析意图拼接为结构化用户提示，引导模型调用工具。
func buildUserPrompt(input agentcore.Input, intent agentcore.Intent) string {
	var out strings.Builder
	out.WriteString("User request:\n")
	out.WriteString(input.Query)
	out.WriteString("\n\nParsed intent:\n")
	out.WriteString(mustJSON(map[string]any{
		"budget_cents": intent.BudgetCents,
		"max_items":    intent.MaxItems,
		"keywords":     intent.Keywords,
		"preferences":  intent.Preferences,
	}))
	out.WriteString("\n\nTask:\n")
	out.WriteString("Call search_products first, then call select_bundle with real candidate IDs. ")
	out.WriteString("Keep the final answer grounded in real tool results.")
	return out.String()
}

// mustJSON 将任意值序列化为带缩进的 JSON 字符串，失败时回退到 fmt 格式。
func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
