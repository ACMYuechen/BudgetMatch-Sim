package llm

import (
	"errors"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"

	"github.com/cloudwego/eino/schema"
)

// TestTrimHistoryByTokenBudgetKeepsLatestCompleteTurns 验证裁剪只保留最近的完整问答轮次。
func TestTrimHistoryByTokenBudgetKeepsLatestCompleteTurns(t *testing.T) {
	history := []*schema.Message{
		schema.UserMessage("第一轮"),
		schema.AssistantMessage(strings.Repeat("旧内容", 20), nil),
		schema.UserMessage("第二轮"),
		schema.AssistantMessage(strings.Repeat("中间内容", 10), nil),
		schema.UserMessage("第三轮"),
		schema.AssistantMessage("最新回答", nil),
	}

	latestCost := estimateMessagesTokens(history[4:])
	got := trimHistoryByTokenBudget(history, latestCost)
	if len(got) != 2 || got[0].Content != "第三轮" || got[1].Content != "最新回答" {
		t.Fatalf("expected latest complete turn, got %+v", got)
	}

	twoTurnsCost := estimateMessagesTokens(history[2:])
	got = trimHistoryByTokenBudget(history, twoTurnsCost)
	if len(got) != 4 || got[0].Content != "第二轮" {
		t.Fatalf("expected latest two complete turns, got %+v", got)
	}
}

// TestTrimHistoryByTokenBudgetDropsOversizedLatestTurn 验证最新一轮自身超限时不留下孤立消息。
func TestTrimHistoryByTokenBudgetDropsOversizedLatestTurn(t *testing.T) {
	history := []*schema.Message{
		schema.UserMessage("问题"),
		schema.AssistantMessage(strings.Repeat("很长的回答", 100), nil),
	}
	if got := trimHistoryByTokenBudget(history, estimateMessageTokens(history[0])); len(got) != 0 {
		t.Fatalf("expected oversized turn to be dropped, got %+v", got)
	}
}

// TestBuildMessagesReservesSystemAndCurrentInput 验证系统提示词与当前问题始终保留，余额用于最近历史。
func TestBuildMessagesReservesSystemAndCurrentInput(t *testing.T) {
	input := agentcore.Input{Query: "预算提高到5000"}
	intent := agentcore.Intent{BudgetCents: 500000, MaxItems: 3, Keywords: []string{"耳机"}}
	history := []*schema.Message{
		schema.UserMessage("第一轮"),
		schema.AssistantMessage("旧回答", nil),
		schema.UserMessage("第二轮"),
		schema.AssistantMessage("最新回答", nil),
	}

	fixedCost := estimateMessageTokens(schema.SystemMessage(systemPrompt)) +
		estimateMessageTokens(schema.UserMessage(buildUserPrompt(input, intent)))
	latestCost := estimateMessagesTokens(history[2:])
	messages, err := buildMessages(input, intent, history, fixedCost+latestCost)
	if err != nil {
		t.Fatalf("buildMessages() error = %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected [system, latest user, latest assistant, current user], got %d", len(messages))
	}
	if messages[0].Role != schema.System || messages[1].Content != "第二轮" || messages[3].Role != schema.User {
		t.Fatalf("unexpected message order: %+v", messages)
	}

	if _, err = buildMessages(input, intent, history, fixedCost-1); !errors.Is(err, agentcore.ErrContextTooLarge) {
		t.Fatalf("oversized fixed input error = %v, want ErrContextTooLarge", err)
	}
}

// TestEstimateTextTokensHandlesChinese 验证估算器不会按 ASCII 比例低估中文文本。
func TestEstimateTextTokensHandlesChinese(t *testing.T) {
	asciiTokens := estimateTextTokens(strings.Repeat("a", 40))
	chineseTokens := estimateTextTokens(strings.Repeat("中", 40))
	if asciiTokens != 10 || chineseTokens != 40 {
		t.Fatalf("unexpected estimates: ascii=%d chinese=%d", asciiTokens, chineseTokens)
	}
}
