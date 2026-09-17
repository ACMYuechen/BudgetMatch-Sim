package recommend

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
)

type agentFunc func(context.Context, agentcore.Input) (*agentcore.Result, error)

func (a agentFunc) Name() string { return "test.func" }
func (a agentFunc) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	return a(ctx, input)
}

func TestGuardrailsInvalidPrimaryFallsBackOnlyOnce(t *testing.T) {
	for _, primaryResult := range []*agentcore.Result{nil, {TotalPriceCents: 1}} {
		calls := 0
		primary := agentFunc(func(context.Context, agentcore.Input) (*agentcore.Result, error) { return primaryResult, nil })
		fallback := agentFunc(func(context.Context, agentcore.Input) (*agentcore.Result, error) {
			calls++
			return &agentcore.Result{}, nil
		})
		result, err := NewService(fallback, primary, nil).Recommend(context.Background(), agentcore.Input{Query: "keyboard"})
		if err != nil || calls != 1 || len(result.ToolsUsed) != 1 || !strings.Contains(result.ToolsUsed[0].Detail, "rejected by constraints") {
			t.Fatalf("invalid primary recovery = %+v, calls=%d, err=%v", result, calls, err)
		}
	}
}

func TestGuardrailsKeepOriginalRequestForIdempotency(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	calls := 0
	a := agentFunc(func(_ context.Context, input agentcore.Input) (*agentcore.Result, error) {
		calls++
		if input.BudgetCents != 1999 || input.MaxItems != 3 {
			t.Fatalf("unresolved execution input: %+v", input)
		}
		return &agentcore.Result{Summary: "ok"}, nil
	})
	s := NewService(a, nil, mem)
	input := agentcore.Input{Query: "预算19.99元买鼠标", UserId: "u", ConversationId: "c", TurnId: "t"}
	for range 2 {
		if _, err := s.Recommend(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	turn, exists, err := mem.FindTurn(context.Background(), "u", "c", "t")
	if err != nil || !exists || turn.BudgetCents != 0 || turn.MaxItems != 0 || calls != 1 || turn.Intent.BudgetCents != 1999 {
		t.Fatalf("canonicalization changed original request: %+v calls=%d err=%v", turn, calls, err)
	}
}

func TestGuardrailsLateCancellationNeverSavesOrFallsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mem := memory.NewInMemory(memory.Conf{})
	primary := agentFunc(func(context.Context, agentcore.Input) (*agentcore.Result, error) {
		cancel()
		return &agentcore.Result{Summary: "late success"}, nil
	})
	fallback := &countingAgent{result: &agentcore.Result{}}
	_, err := NewService(fallback, primary, mem).Recommend(ctx, agentcore.Input{Query: "keyboard", UserId: "u", ConversationId: "c", TurnId: "t"})
	if !errors.Is(err, context.Canceled) || fallback.calls != 0 {
		t.Fatalf("late cancellation accepted: calls=%d err=%v", fallback.calls, err)
	}
	if _, found, err := mem.FindTurn(context.Background(), "u", "c", "t"); err != nil || found {
		t.Fatalf("late result saved: %v, %v", found, err)
	}
}

func TestGuardrailsServiceRejectsInvalidFallbackBeforeSaving(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	fallback := &stubAgent{name: "invalid", result: &agentcore.Result{
		Items: []agentcore.BundleItem{{Id: "invented", PriceCents: 100, Stock: 1}}, TotalPriceCents: 100,
	}}
	s := NewService(fallback, nil, mem)
	_, err := s.Recommend(context.Background(), agentcore.Input{
		Query: "keyboard", BudgetCents: 50, MaxItems: 1, UserId: "u", ConversationId: "c", TurnId: "t",
	})
	if err == nil {
		t.Fatal("invalid fallback was accepted")
	}
	if _, found, err := mem.FindTurn(context.Background(), "u", "c", "t"); err != nil || found {
		t.Fatalf("invalid result persisted: found=%v, err=%v", found, err)
	}
}

func TestGuardrailsTextAndStoredConstraintsValidatedBeforeExecution(t *testing.T) {
	for _, input := range []agentcore.Input{
		{Query: "预算 1000000001 元买键盘"},
		{Query: "预算 999999999999999999999999 万买键盘"},
		{Query: "便携一点", PriorIntent: &agentcore.Intent{BudgetCents: maxBudgetCents + 1, MaxItems: 2}},
		{Query: "便携一点", PriorIntent: &agentcore.Intent{BudgetCents: 100, MaxItems: 11}},
	} {
		a := &countingAgent{result: &agentcore.Result{}}
		_, err := NewService(a, nil, nil).Recommend(context.Background(), input)
		if !errors.Is(err, agentcore.ErrInvalidInput) || a.calls != 0 {
			t.Fatalf("invalid constraints executed: input=%+v, calls=%d, err=%v", input, a.calls, err)
		}
	}
}

func TestGuardrailsTerminalErrorsDoNotStartFallback(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, agentcore.ErrInvalidInput} {
		fallback := &countingAgent{result: &agentcore.Result{}}
		primary := &stubAgent{name: "primary", err: cause}
		_, err := NewService(fallback, primary, nil).Recommend(context.Background(), agentcore.Input{Query: "keyboard"})
		if !errors.Is(err, cause) || fallback.calls != 0 {
			t.Fatalf("terminal error fell back: cause=%v calls=%d err=%v", cause, fallback.calls, err)
		}
	}
}
