package recommend

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"

	"github.com/cloudwego/eino/schema"
)

// TestServiceUsesFallbackWhenNoPrimary 验证未配置 primary 时，服务直接使用规则兜底 Agent。
func TestServiceUsesFallbackWhenNoPrimary(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	service := NewService(fallback, nil, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "rule" {
		t.Fatalf("expected fallback summary, got %q", result.Summary)
	}
}

// TestServicePrefersPrimary 验证 primary 可用时优先返回其结果。
func TestServicePrefersPrimary(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	primary := &stubAgent{name: "llm", result: &agentcore.Result{Summary: "react"}}
	service := NewService(fallback, primary, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "react" {
		t.Fatalf("expected primary summary, got %q", result.Summary)
	}
}

// TestServiceFallsBackWhenPrimaryFails 验证 primary 失败时降级到兜底，并记录失败原因。
func TestServiceFallsBackWhenPrimaryFails(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	primary := &stubAgent{name: "llm", err: errors.New("model down")}
	service := NewService(fallback, primary, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.Summary != "rule" {
		t.Fatalf("expected fallback summary, got %q", result.Summary)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0].Success {
		t.Fatalf("expected failed primary record, got %+v", result.ToolsUsed)
	}
	if result.ToolsUsed[0].Name != "primary.llm" || result.ToolsUsed[0].Detail != "model down" {
		t.Fatalf("unexpected fallback tool record: %+v", result.ToolsUsed[0])
	}
}

// TestServiceGeneratesConversationId 验证未携带会话 ID 时服务端生成并回传。
func TestServiceGeneratesConversationId(t *testing.T) {
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}
	service := NewService(fallback, nil, nil)

	result, err := service.Recommend(context.Background(), agentcore.Input{Query: "study"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if result.ConversationId == "" {
		t.Fatal("expected generated conversation id")
	}

	// 已携带会话 ID 时原样回传。
	again, err := service.Recommend(context.Background(), agentcore.Input{Query: "study", ConversationId: "c-given"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}
	if again.ConversationId != "c-given" {
		t.Fatalf("expected conversation id passthrough, got %q", again.ConversationId)
	}
}

// TestServiceWritesMemoryOnSuccess 验证成功路径把裸 Query 与 Summary 成对写入记忆。
func TestServiceWritesMemoryOnSuccess(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	primary := &stubAgent{name: "llm", result: &agentcore.Result{Summary: "react summary"}}
	service := NewService(&stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}, primary, mem)

	_, err := service.Recommend(context.Background(), agentcore.Input{Query: "预算3000买键盘", UserId: "u1", ConversationId: "c1"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}

	history, err := mem.History(context.Background(), "u1", "c1", 0)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages in memory, got %d", len(history))
	}
	if history[0].Role != schema.User || history[0].Content != "预算3000买键盘" {
		t.Fatalf("expected raw user query in memory, got %+v", history[0])
	}
	if history[1].Role != schema.Assistant || history[1].Content != "react summary" {
		t.Fatalf("expected assistant summary in memory, got %+v", history[1])
	}
}

// TestServiceWritesMemoryOnFallback 验证降级路径同样写入记忆，历史无空洞。
func TestServiceWritesMemoryOnFallback(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	primary := &stubAgent{name: "llm", err: errors.New("model down")}
	fallback := &stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule summary"}}
	service := NewService(fallback, primary, mem)

	_, err := service.Recommend(context.Background(), agentcore.Input{Query: "study", UserId: "u1", ConversationId: "c1"})
	if err != nil {
		t.Fatalf("Recommend() error = %v", err)
	}

	history, err := mem.History(context.Background(), "u1", "c1", 0)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 2 || history[1].Content != "rule summary" {
		t.Fatalf("expected fallback round recorded in memory, got %+v", history)
	}
}

// TestServiceKeepsTitleAfterHistoryTruncation 验证滚动窗口移除首轮消息后，会话标题仍保持首次值。
func TestServiceKeepsTitleAfterHistoryTruncation(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{MaxHistory: 2})
	service := NewService(&stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}, nil, mem)

	for i, query := range []string{"第一轮需求", "第二轮追问", "第三轮追问"} {
		result, err := service.Recommend(context.Background(), agentcore.Input{
			Query:          query,
			UserId:         "u1",
			ConversationId: "c1",
		})
		if err != nil {
			t.Fatalf("Recommend(round %d) error = %v", i+1, err)
		}
		if result.ConversationTitle != "第一轮需求" {
			t.Fatalf("round %d title changed to %q", i+1, result.ConversationTitle)
		}
	}
}

// TestServiceFallbackUsesConversationHistory 验证无模型与模型失败两条规则路径都继承历史商品上下文。
func TestServiceFallbackUsesConversationHistory(t *testing.T) {
	tests := []struct {
		name    string
		primary agentcore.Agent
	}{
		{name: "no primary"},
		{name: "primary failure", primary: &stubAgent{name: "llm", err: errors.New("model down")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := memory.NewInMemory(memory.Conf{})
			fallback := NewAgent(tools.NewMockProductProvider(), selector.NewBundleSelector()).WithMemory(mem, 20)
			service := NewService(fallback, tt.primary, mem)

			if _, err := service.Recommend(context.Background(), agentcore.Input{
				Query:          "预算3000买通勤耳机",
				UserId:         "u1",
				ConversationId: "c1",
			}); err != nil {
				t.Fatalf("first Recommend() error = %v", err)
			}

			result, err := service.Recommend(context.Background(), agentcore.Input{
				Query:          "预算提高到5000",
				UserId:         "u1",
				ConversationId: "c1",
			})
			if err != nil {
				t.Fatalf("follow-up Recommend() error = %v", err)
			}
			if result.Intent.BudgetCents != 500000 {
				t.Fatalf("BudgetCents = %d, want 500000", result.Intent.BudgetCents)
			}
			if !containsString(result.Intent.Keywords, "耳机") {
				t.Fatalf("expected inherited headphones keyword, got %v", result.Intent.Keywords)
			}
		})
	}
}

func TestServiceTurnIdIsIdempotent(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	agent := &countingAgent{result: &agentcore.Result{Intent: agentcore.Intent{BudgetCents: 300000, MaxItems: 3}, Summary: "first"}}
	service := NewService(agent, nil, mem)
	input := agentcore.Input{Query: "预算3000买耳机", UserId: "u1", ConversationId: "c1", TurnId: "turn-1"}

	first, err := service.Recommend(context.Background(), input)
	if err != nil {
		t.Fatalf("first Recommend() error = %v", err)
	}
	agent.result.Summary = "should not replace"
	retried, err := service.Recommend(context.Background(), input)
	if err != nil {
		t.Fatalf("retried Recommend() error = %v", err)
	}
	if agent.calls != 1 || first.Summary != "first" || retried.Summary != "first" || retried.TurnId != "turn-1" {
		t.Fatalf("idempotent replay failed: calls=%d first=%+v retried=%+v", agent.calls, first, retried)
	}
	_, turns, total, exists, err := mem.ListTurns(context.Background(), "u1", "c1", 1, 20)
	if err != nil || !exists || total != 1 || len(turns) != 1 {
		t.Fatalf("stored turns = %d/%d exists=%v err=%v", len(turns), total, exists, err)
	}
}

func TestServiceTurnIdRejectsDifferentRequest(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	agent := &countingAgent{result: &agentcore.Result{Intent: agentcore.Intent{BudgetCents: 300000, MaxItems: 3}, Summary: "first"}}
	service := NewService(agent, nil, mem)
	first := agentcore.Input{Query: "预算3000买耳机", BudgetCents: 300000, MaxItems: 3, UserId: "u1", ConversationId: "c1", TurnId: "turn-1"}
	if _, err := service.Recommend(context.Background(), first); err != nil {
		t.Fatalf("first Recommend() error = %v", err)
	}

	changed := first
	changed.Query = "预算3000买键盘"
	if _, err := service.Recommend(context.Background(), changed); !errors.Is(err, agentcore.ErrTurnConflict) {
		t.Fatalf("changed Recommend() error = %v, want ErrTurnConflict", err)
	}
	if agent.calls != 1 {
		t.Fatalf("conflicting request executed agent, calls = %d", agent.calls)
	}
}

func TestServiceRejectsInvalidInput(t *testing.T) {
	service := NewService(&stubAgent{name: "fallback", result: &agentcore.Result{Summary: "rule"}}, nil, nil)
	tests := []struct {
		name  string
		input agentcore.Input
	}{
		{name: "blank query", input: agentcore.Input{Query: " \n\t"}},
		{name: "query too long", input: agentcore.Input{Query: strings.Repeat("问", maxQueryRunes+1)}},
		{name: "negative budget", input: agentcore.Input{Query: "耳机", BudgetCents: -1}},
		{name: "budget too large", input: agentcore.Input{Query: "耳机", BudgetCents: maxBudgetCents + 1}},
		{name: "negative items", input: agentcore.Input{Query: "耳机", MaxItems: -1}},
		{name: "too many items", input: agentcore.Input{Query: "耳机", MaxItems: maxRequestItems + 1}},
		{name: "blank conversation id", input: agentcore.Input{Query: "耳机", ConversationId: "   "}},
		{name: "conversation id too long", input: agentcore.Input{Query: "耳机", ConversationId: strings.Repeat("c", maxIDRunes+1)}},
		{name: "turn id with surrounding whitespace", input: agentcore.Input{Query: "耳机", TurnId: " turn-1"}},
		{name: "turn id too long", input: agentcore.Input{Query: "耳机", TurnId: strings.Repeat("t", maxIDRunes+1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Recommend(context.Background(), test.input); !errors.Is(err, agentcore.ErrInvalidInput) {
				t.Fatalf("Recommend() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestServiceConversationOperationsRejectInvalidID(t *testing.T) {
	service := NewService(&stubAgent{name: "fallback"}, nil, memory.NewInMemory(memory.Conf{}))
	for _, conversationId := range []string{"", "   ", " conversation-1", strings.Repeat("c", maxIDRunes+1)} {
		if _, _, _, _, err := service.ListTurns(context.Background(), "u1", conversationId, 1, 20); !errors.Is(err, agentcore.ErrInvalidInput) {
			t.Fatalf("ListTurns(%q) error = %v, want ErrInvalidInput", conversationId, err)
		}
		if _, err := service.DeleteConversation(context.Background(), "u1", conversationId); !errors.Is(err, agentcore.ErrInvalidInput) {
			t.Fatalf("DeleteConversation(%q) error = %v, want ErrInvalidInput", conversationId, err)
		}
	}
}

// stubAgent 是用于测试的 agentcore.Agent 实现。
type stubAgent struct {
	name   string
	result *agentcore.Result
	err    error
}

type countingAgent struct {
	calls  int
	result *agentcore.Result
}

func (a *countingAgent) Name() string { return "counting" }

func (a *countingAgent) Run(context.Context, agentcore.Input) (*agentcore.Result, error) {
	a.calls++
	clone := *a.result
	clone.Intent.Keywords = append([]string(nil), a.result.Intent.Keywords...)
	return &clone, nil
}

// Name 返回测试 Agent 名称。
func (a *stubAgent) Name() string { return a.name }

// Run 返回预置结果或错误。
func (a *stubAgent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.err != nil {
		return nil, a.err
	}
	// 返回副本，避免测试间共享同一结果指针被追加修改。
	clone := *a.result
	clone.ToolsUsed = append([]agentcore.ToolCall(nil), a.result.ToolsUsed...)
	return &clone, nil
}
