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
- Use read_file and write_file to load user preferences, save recommendations, or handle document-based tasks.
  All file paths must be relative to the configured agent workspace directory. Never use absolute paths or "..".
  Do not include the workspace directory itself in a tool path.
  write_file only accepts .json, .md, and .txt files.
  Conventions:
    preferences.json — user shopping preferences
    recommendations/latest.md — latest saved recommendation report
  When the user says "my preferences" or "last saved", read or write these well-known paths.
  If a path is unclear, ask the user where the file is.
- Use external MCP tools only when the user's goal clearly benefits from them.
- If the product list is insufficient, say what is missing and keep the recommendation conservative.
- In multi-turn conversations, interpret follow-up requests (budget changes, item swaps, "the previous one") relative to the conversation history.
- Return concise Chinese output for end users unless the user explicitly asks for another language.

Expected result:
- Summarize the user's intent.
- Select 1 to max_items products.
- Explain why each product belongs in the bundle.
- Mention total price in cents and yuan.
- Mention which tools or data sources were used.`

// buildMessages 根据用户输入、解析意图与会话历史构建 Eino 对话消息列表。
// 结构为 [system, ...history, currentUser]：历史消息是既往轮次的裸问答对，
// 意图脚手架只拼在当前轮的 user 消息里，不会随历史逐轮累积；历史按近似 token
// 预算从最新完整问答轮次向前保留。系统提示词和当前问题不会被静默截断；
// 二者自身已超过上限时直接返回 ErrContextTooLarge，使配置成为严格总上限。
func buildMessages(input agentcore.Input, intent agentcore.Intent, history []*schema.Message, maxContextTokens int) ([]*schema.Message, error) {
	systemMessage := schema.SystemMessage(systemPrompt)
	currentMessage := schema.UserMessage(buildUserPrompt(input, intent))
	fixedCost := estimateMessageTokens(systemMessage) + estimateMessageTokens(currentMessage)
	if maxContextTokens > 0 && fixedCost > maxContextTokens {
		return nil, fmt.Errorf("%w: estimated %d tokens, limit %d", agentcore.ErrContextTooLarge, fixedCost, maxContextTokens)
	}
	historyBudget := maxContextTokens - fixedCost
	trimmedHistory := trimHistoryByTokenBudget(history, historyBudget)

	messages := make([]*schema.Message, 0, len(trimmedHistory)+2)
	messages = append(messages, systemMessage)
	messages = append(messages, trimmedHistory...)
	messages = append(messages, currentMessage)
	return messages, nil
}

// trimHistoryByTokenBudget 从最近一轮开始保留完整对话轮次，预算不足时丢弃更早历史。
func trimHistoryByTokenBudget(history []*schema.Message, budget int) []*schema.Message {
	if budget <= 0 || len(history) == 0 {
		return nil
	}

	turns := splitHistoryTurns(history)
	start := len(turns)
	used := 0
	for index := len(turns) - 1; index >= 0; index-- {
		cost := estimateMessagesTokens(turns[index])
		if used+cost > budget {
			break
		}
		used += cost
		start = index
	}

	var selected []*schema.Message
	for _, turn := range turns[start:] {
		selected = append(selected, turn...)
	}
	return selected
}

// splitHistoryTurns 以 user 消息为每轮起点，确保裁剪不会留下孤立的 assistant/tool 消息。
func splitHistoryTurns(history []*schema.Message) [][]*schema.Message {
	turns := make([][]*schema.Message, 0, len(history)/2)
	for _, msg := range history {
		if msg == nil {
			continue
		}
		if msg.Role == schema.User {
			turns = append(turns, []*schema.Message{msg})
			continue
		}
		if len(turns) > 0 {
			last := len(turns) - 1
			turns[last] = append(turns[last], msg)
		}
	}
	return turns
}

// estimateMessagesTokens 返回消息切片的模型无关近似 token 数。
func estimateMessagesTokens(messages []*schema.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageTokens(msg)
	}
	return total
}

// estimateMessageTokens 通过消息 JSON 估算 token，覆盖文本、角色与工具调用参数。
func estimateMessageTokens(msg *schema.Message) int {
	if msg == nil {
		return 0
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return estimateTextTokens(msg.Content) + 4
	}
	return estimateTextTokens(string(data)) + 4
}

// estimateTextTokens 使用“ASCII 约 4 字符/token、非 ASCII 约 1 字符/token”的保守近似。
// 实际 tokenizer 因模型而异，因此该预算用于稳定裁剪，不作为计费统计。
func estimateTextTokens(text string) int {
	ascii := 0
	tokens := 0
	for _, value := range text {
		if value <= 0x7f {
			ascii++
		} else {
			tokens++
		}
	}
	return tokens + (ascii+3)/4
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
	out.WriteString("Use read_file/write_file when the user asks to load or save files. ")
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
