package recommend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"github.com/cloudwego/eino/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type demandFinalizerFunc func(context.Context, *agent.Result) error

func (f demandFinalizerFunc) Finalize(ctx context.Context, result *agent.Result) error {
	return f(ctx, result)
}

func demandInput() agent.Input {
	return agent.Input{Query: "预算1000元买键盘", UserId: "u", ConversationId: "c", TurnId: "t"}
}

func TestPlanDemandPersistsWithoutExecutingRecommendation(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	runner := agentFunc(func(context.Context, agent.Input) (*agent.Result, error) {
		t.Fatal("planning invoked an agent")
		return nil, nil
	})
	s := NewService(runner, runner, mem).WithFinalizer(demandFinalizerFunc(func(context.Context, *agent.Result) error {
		t.Fatal("planning invoked finalizer")
		return nil
	}))
	input := demandInput()
	r, err := s.PlanDemand(context.Background(), input, `{"schema_version":1,"required":{"operation":"add","values":["keyboard"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "intent_ready" || r.Intent.BudgetCents != 100000 || r.Intent.Demand.Required[0] != "keyboard" || len(r.Items) != 0 || len(r.ToolsUsed) != 0 || r.TotalPriceCents != 0 {
		t.Fatalf("not a planning-only result: %+v", r)
	}
	turn, found, err := mem.FindTurn(context.Background(), "u", "c", "t")
	if err != nil || !found || turn.BudgetCents != 0 || !strings.Contains(string(turn.ResultJSON), `"_demand_request_sha256"`) {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	public, _ := json.Marshal(r)
	if strings.Contains(string(public), "sha256") {
		t.Fatal("private metadata leaked")
	}
	input.TurnId = "next"
	if _, err := s.Recommend(context.Background(), input); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("legacy execution not blocked: %v", err)
	}
	if _, found, _ := mem.FindTurn(context.Background(), "u", "c", "next"); found {
		t.Fatal("blocked request saved")
	}
}

func TestPlanDemandCanonicalReplayAndMethodConflict(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	s := NewService(&stubAgent{result: &agent.Result{}}, nil, mem)
	input := demandInput()
	first, err := s.PlanDemand(context.Background(), input, `{"schema_version":1,"required":{"operation":"add","values":["mouse","keyboard"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.PlanDemand(context.Background(), input, ` { "required": {"values":["keyboard","mouse"],"operation":"add"},"schema_version":1 } `)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("replay differs: %v, %+v", err, again)
	}
	for _, raw := range []string{`{"schema_version":1}`, `{"schema_version":1,"required":{"operation":"replace","values":["keyboard","mouse"]}}`} {
		if _, err := s.PlanDemand(context.Background(), input, raw); !errors.Is(err, agent.ErrTurnConflict) {
			t.Fatalf("patch conflict = %v", err)
		}
	}
	if _, err := s.Recommend(context.Background(), input); !errors.Is(err, agent.ErrTurnConflict) {
		t.Fatalf("method conflict = %v", err)
	}
	input.TurnId = "empty"
	if _, err := s.PlanDemand(context.Background(), input, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanDemand(context.Background(), input, `{"schema_version":1}`); err != nil {
		t.Fatal(err)
	}
	input.BudgetCents = first.Intent.BudgetCents
	if _, err := s.PlanDemand(context.Background(), input, ""); !errors.Is(err, agent.ErrTurnConflict) {
		t.Fatalf("raw numeric identity lost: %v", err)
	}
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if conversation.TurnCount != 2 {
		t.Fatalf("retries duplicated turns: %+v", conversation)
	}
	input = demandInput()
	input.ConversationId = "legacy"
	if _, err := s.Recommend(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanDemand(context.Background(), input, ""); !errors.Is(err, agent.ErrTurnConflict) {
		t.Fatalf("legacy turn reused by planning: %v", err)
	}
}

func TestPlanDemandConflictRollsBackWholeIntentAndReplays(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	s := NewService(nil, nil, mem)
	input := demandInput()
	base, err := s.PlanDemand(context.Background(), input, `{"schema_version":1,"required":{"operation":"replace","values":["keyboard","mouse"]},"preferences":{"operation":"add","values":["quiet"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	input.Query, input.BudgetCents, input.MaxItems, input.TurnId = "phone", 123, 1, "conflict"
	patch := `{"schema_version":1,"excluded":{"operation":"add","values":["keyboard"]},"preferences":{"operation":"replace","values":[]}}`
	for range 2 {
		r, err := s.PlanDemand(context.Background(), input, patch)
		if err != nil || r.Status != "needs_clarification" || !reflect.DeepEqual(base.Intent, r.Intent) || len(r.DemandConflicts) != 2 || len(r.Items) != 0 {
			t.Fatalf("partial state applied: %+v err=%v", r, err)
		}
	}
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if conversation.TurnCount != 2 || !reflect.DeepEqual(intentFromState(conversation.State), base.Intent) {
		t.Fatalf("conflict state mutated: %+v", conversation)
	}
}

func TestPlanDemandFirstConflictCannotBypassClarification(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	fallback := &countingAgent{result: &agent.Result{}}
	s := NewService(fallback, nil, mem)
	input := demandInput()
	input.MaxItems = 1
	r, err := s.PlanDemand(context.Background(), input, `{"schema_version":1,"required":{"operation":"add","values":["keyboard","mouse"]}}`)
	if err != nil || r.Status != "needs_clarification" || r.Intent.Demand != nil {
		t.Fatalf("first conflict changed active intent: %+v %v", r, err)
	}
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if !conversation.State.PlanningOnly {
		t.Fatal("first conflict did not preserve planning mode")
	}
	input.TurnId = "bypass"
	if _, err := s.Recommend(context.Background(), input); status.Code(err) != codes.FailedPrecondition || fallback.calls != 0 {
		t.Fatalf("clarification bypass: %v calls=%d", err, fallback.calls)
	}
	if _, exists, _ := mem.FindTurn(context.Background(), "u", "c", "bypass"); exists {
		t.Fatal("bypass persisted")
	}
}

func TestPlanDemandWithdrawalsAreNotResurrected(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{MaxHistory: 2})
	s := NewService(nil, nil, mem)
	input := demandInput()
	patches := []string{
		`{"schema_version":1,"required":{"operation":"add","values":["keyboard"]},"preferences":{"operation":"replace","values":["quiet","portable"]}}`,
		`{"schema_version":1,"required":{"operation":"remove","values":["keyboard"]},"preferences":{"operation":"remove","values":["quiet"]}}`,
		`{"schema_version":1,"preferences":{"operation":"replace","values":[]}}`,
		`{"schema_version":1}`,
	}
	for i, patch := range patches {
		input.TurnId, input.Query = fmt.Sprint(i), "quiet portable keyboard"
		r, err := s.PlanDemand(context.Background(), input, patch)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && !reflect.DeepEqual(r.Intent.Preferences, []string{"portable"}) {
			t.Fatalf("remove lost: %+v", r.Intent)
		}
		if i >= 2 && (len(r.Intent.Preferences) != 0 || len(r.Intent.Demand.Required) != 0) {
			t.Fatalf("removed demand resurrected: %+v", r.Intent)
		}
	}
}

func TestPlanDemandLegacyMigrationAndUnsupportedPreference(t *testing.T) {
	for _, values := range [][]string{{"便携", "portable", "续航"}, {"brand"}} {
		prior := agent.Intent{BudgetCents: 700, MaxItems: 2, Keywords: []string{"keyboard"}, Preferences: values}
		input := agent.Input{Query: "继续", PriorIntent: &prior}
		r, err := NewPlanner().ResolveDemand(input, nil, demand.Patch{SchemaVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
		if values[0] == "brand" {
			if r.Status != "needs_clarification" || !reflect.DeepEqual(r.Intent, prior) {
				t.Fatalf("unsupported preference silently lost: %+v", r)
			}
			patch := demand.Patch{SchemaVersion: 1, Preferences: &demand.Change[demand.Preference]{Operation: demand.Replace, Values: []demand.Preference{}}}
			r, err = NewPlanner().ResolveDemand(input, nil, patch)
			if err != nil || r.Status != "intent_ready" || len(r.Intent.Preferences) != 0 {
				t.Fatalf("explicit replacement failed: %+v %v", r, err)
			}
		} else if !reflect.DeepEqual(r.Intent.Preferences, []string{"battery_life", "portable"}) {
			t.Fatalf("migration=%+v", r.Intent)
		}
		if !reflect.DeepEqual(prior.Preferences, values) {
			t.Fatal("prior input mutated")
		}
	}
}

func TestPlanDemandRestoresTextOnlyAndPartialLegacyConversations(t *testing.T) {
	ctx := context.Background()
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%v", partial), func(t *testing.T) {
			mem := memory.NewInMemory(memory.Conf{})
			query := "预算800元，最多二件，便携续航"
			if _, err := mem.GetOrCreateTitle(ctx, "u", "c", "legacy title"); err != nil {
				t.Fatal(err)
			}
			if err := mem.Append(ctx, "u", "c", schema.UserMessage(query), schema.AssistantMessage("legacy", nil)); err != nil {
				t.Fatal(err)
			}
			wantBudget := int64(80000)
			if partial {
				wantBudget = 50000
				_, _, err := mem.SaveTurn(ctx, memory.SaveTurnReq{UserId: "u", ConversationId: "c", TurnId: "legacy", Query: query, Intent: memory.IntentState{BudgetCents: wantBudget}, ResultJSON: json.RawMessage(`{}`)})
				if err != nil {
					t.Fatal(err)
				}
			}
			input := demandInput()
			input.Query = "继续"
			r, err := NewService(nil, nil, mem).PlanDemand(ctx, input, "")
			if err != nil || r.Intent.BudgetCents != wantBudget || r.Intent.MaxItems != 2 || r.ConversationTitle != "legacy title" || !reflect.DeepEqual(r.Intent.Preferences, []string{"battery_life", "portable"}) {
				t.Fatalf("legacy history lost: %+v %v", r, err)
			}
		})
	}
}

func TestPlanDemandRejectsInvalidPatchBeforeStorage(t *testing.T) {
	for _, raw := range []string{
		`{}`, `null`, `{"schema_version":2}`, `{"schema_version":1,"schema_version":1}`,
		`{"schema_version":1,"budget_cents":100}`, `{"schema_version":1,"max_items":2}`,
		`{"schema_version":1,"required":null}`, `{"schema_version":1,"private":"SECRET"}`,
		`{"schema_version":1,"Required":{"operation":"replace","values":[]}}`,
		`{"schema_version":1,"required":{"operation":"add","values":["unknown"]}}`,
		`{"schema_version":1,"preferences":{"operation":"add","values":["quiet","quiet"]}}`,
		strings.Repeat(" ", demand.MaxPatchBytes+1), string([]byte{0xff}),
	} {
		mem := memory.NewInMemory(memory.Conf{})
		_, err := NewService(nil, nil, mem).PlanDemand(context.Background(), demandInput(), raw)
		if !errors.Is(err, agent.ErrInvalidInput) || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("invalid input error=%v", err)
		}
		if _, found, _ := mem.GetConversation(context.Background(), "u", "c"); found {
			t.Fatal("invalid patch wrote state")
		}
	}
}

func TestPlanDemandRejectsCorruptState(t *testing.T) {
	for _, prior := range []agent.Intent{
		{BudgetCents: 100, MaxItems: 2, Demand: &agent.DemandState{SchemaVersion: 2}},
		{BudgetCents: 0, MaxItems: 2, Demand: &agent.DemandState{SchemaVersion: 1}},
		{BudgetCents: 100, MaxItems: 2, Preferences: []string{"brand"}, Demand: &agent.DemandState{SchemaVersion: 1}},
		{BudgetCents: 100, MaxItems: 2, Demand: &agent.DemandState{SchemaVersion: 1, Required: []string{"keyboard"}, Excluded: []string{"keyboard"}}},
	} {
		_, err := NewPlanner().ResolveDemand(agent.Input{Query: "继续", PriorIntent: &prior}, nil, demand.Patch{SchemaVersion: 1})
		if !errors.Is(err, agent.ErrUnsafeResult) {
			t.Fatalf("corrupt state accepted: %+v %v", prior, err)
		}
	}
}

type demandFailStore struct {
	memory.ConversationStore
	failHistory bool
	cancel      context.CancelFunc
}

type demandCommitCancelStore struct {
	memory.ConversationStore
	cancel context.CancelFunc
}

func (s demandCommitCancelStore) SaveTurn(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	conversation, turn, err := s.ConversationStore.SaveTurn(ctx, req)
	s.cancel()
	return conversation, turn, err
}

func TestPlanDemandCancellationAfterCommitCanReplay(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewService(nil, nil, demandCommitCancelStore{ConversationStore: mem, cancel: cancel})
	r, err := s.PlanDemand(ctx, demandInput(), "")
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("late cancellation reported success: %+v %v", r, err)
	}
	r, err = s.PlanDemand(context.Background(), demandInput(), "")
	if err != nil || r.Status != "intent_ready" {
		t.Fatalf("committed result not replayable: %+v %v", r, err)
	}
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if conversation.TurnCount != 1 {
		t.Fatal("retry after late cancellation duplicated commit")
	}
}

func (s demandFailStore) History(context.Context, string, string, int) ([]*schema.Message, error) {
	if s.failHistory {
		return nil, errors.New("history unavailable")
	}
	if s.cancel != nil {
		s.cancel()
	}
	return nil, nil
}

func (s demandFailStore) SaveTurn(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	return memory.Conversation{}, memory.Turn{}, errors.New("save unavailable")
}

func TestPlanDemandRequiresDurableSuccessAndHandlesCancellation(t *testing.T) {
	if _, err := NewService(nil, nil, nil).PlanDemand(context.Background(), demandInput(), ""); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("no store error=%v", err)
	}
	for _, mode := range []string{"history", "save", "cancel", "already_canceled"} {
		t.Run(mode, func(t *testing.T) {
			mem := memory.NewInMemory(memory.Conf{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := demandFailStore{ConversationStore: mem, failHistory: mode == "history"}
			if mode == "cancel" {
				store.cancel = cancel
			}
			if mode == "already_canceled" {
				cancel()
			}
			r, err := NewService(nil, nil, store).PlanDemand(ctx, demandInput(), "")
			if err == nil || r != nil {
				t.Fatalf("failed persistence reported success: %+v %v", r, err)
			}
			if strings.Contains(mode, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, found, _ := mem.GetConversation(context.Background(), "u", "c"); found {
				t.Fatal("failed request saved")
			}
		})
	}
}

func TestPlanDemandSerializesConcurrentTurnsAndIsolatesUsers(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	s := NewService(nil, nil, mem)
	var wg sync.WaitGroup
	for i, category := range []string{"keyboard", "mouse", "lighting"} {
		wg.Add(1)
		go func(i int, category string) {
			defer wg.Done()
			input := demandInput()
			input.TurnId = fmt.Sprint(i)
			_, err := s.PlanDemand(context.Background(), input, fmt.Sprintf(`{"schema_version":1,"required":{"operation":"add","values":[%q]}}`, category))
			if err != nil {
				t.Error(err)
			}
		}(i, category)
	}
	wg.Wait()
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if conversation.TurnCount != 3 || len(conversation.State.Demand.Required) != 3 {
		t.Fatalf("lost concurrent patch: %+v", conversation)
	}
	input := demandInput()
	input.UserId = "another"
	r, err := s.PlanDemand(context.Background(), input, "")
	if err != nil || len(r.Intent.Demand.Required) != 0 {
		t.Fatalf("cross-user state leak: %+v %v", r, err)
	}
	if _, err := s.DeleteConversation(context.Background(), "u", "c"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := mem.GetConversation(context.Background(), "another", "c"); !found {
		t.Fatal("cross-user deletion")
	}
}

func TestDemandGuardBlocksDirectRuleAndSelectorPaths(t *testing.T) {
	intent := agent.Intent{BudgetCents: 1000, MaxItems: 2, Demand: &agent.DemandState{SchemaVersion: 1}}
	provider := &countingProductProvider{}
	_, err := NewAgent(provider, selector.NewBundleSelector()).Run(context.Background(), agent.Input{Query: "desk", PriorIntent: &intent})
	if status.Code(err) != codes.FailedPrecondition || provider.calls != 0 {
		t.Fatalf("direct rule ignored demand: %v calls=%d", err, provider.calls)
	}
	items, total := selector.NewBundleSelector().Select([]agent.ProductCandidate{{Id: "a", PriceCents: 10, Stock: 1}}, intent)
	if len(items) != 0 || total != 0 {
		t.Fatal("legacy selector ignored demand")
	}
}

func TestPlanDemandNumericPrecedenceAndLegacyConflict(t *testing.T) {
	prior := agent.Intent{BudgetCents: 70000, MaxItems: 2, Keywords: []string{"keyboard"}, Preferences: []string{"便携"}}
	for _, tc := range []struct {
		query    string
		explicit int64
		want     int64
	}{
		{"预算300元", 20000, 20000}, {"预算300元", 0, 30000}, {"继续", 0, 70000},
	} {
		r, err := NewPlanner().ResolveDemand(agent.Input{Query: tc.query, BudgetCents: tc.explicit, PriorIntent: &prior}, nil, demand.Patch{SchemaVersion: 1})
		if err != nil || r.Intent.BudgetCents != tc.want {
			t.Fatalf("numeric precedence: %+v %v", r, err)
		}
	}
	patch, _, err := decodeDemandRequest(`{"schema_version":1,"required":{"operation":"add","values":["keyboard","mouse","lighting"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewPlanner().ResolveDemand(agent.Input{Query: "预算300元 phone", PriorIntent: &prior}, nil, patch)
	if err != nil || r.Status != "needs_clarification" || !reflect.DeepEqual(r.Intent, prior) {
		t.Fatalf("legacy rollback changed state: %+v %v", r, err)
	}
	r, err = NewPlanner().ResolveDemand(agent.Input{Query: "继续"}, []string{"预算800元，最多二件"}, demand.Patch{SchemaVersion: 1})
	if err != nil || r.Intent.BudgetCents != 80000 || r.Intent.MaxItems != 2 {
		t.Fatalf("history limits lost: %+v %v", r, err)
	}
}

func TestPlanDemandConcurrentRetrySavesOnce(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	s := NewService(nil, nil, mem)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.PlanDemand(context.Background(), demandInput(), ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
	if conversation.TurnCount != 1 {
		t.Fatalf("retries created %d turns", conversation.TurnCount)
	}
}

func FuzzDemandRequestCanonical(f *testing.F) {
	for _, raw := range []string{"", `{"schema_version":1}`, `{"schema_version":1,"required":{"operation":"add","values":["mouse","keyboard"]}}`, `{"schema_version":1,"preferences":{"operation":"replace","values":[]}}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		patch, fingerprint, err := decodeDemandRequest(raw)
		if err != nil {
			return
		}
		data, err := json.Marshal(patch)
		if err != nil {
			t.Fatal(err)
		}
		_, again, err := decodeDemandRequest(string(data))
		if err != nil || again != fingerprint || len(fingerprint) != 64 {
			t.Fatalf("unstable canonical request: %v", err)
		}
	})
}
