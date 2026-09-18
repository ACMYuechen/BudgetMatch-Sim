package memory

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"
)

func testDemandState() IntentState {
	return IntentState{BudgetCents: 10000, MaxItems: 3, Keywords: []string{"desk"}, Preferences: []string{"quiet"},
		PlanningOnly: true,
		Demand:       &agent.DemandState{SchemaVersion: 1, Required: []string{"keyboard"}, Optional: []string{"mouse"}, Excluded: []string{"phone"}}}
}

func mutateDemandState(state IntentState) {
	state.Keywords[0], state.Preferences[0] = "mutated", "mutated"
	state.Demand.Required[0], state.Demand.Optional[0], state.Demand.Excluded[0] = "mutated", "mutated", "mutated"
	state.Demand.SchemaVersion = 999
}

func TestDemandMemoryRoundTripAndDeepCopy(t *testing.T) {
	ctx := context.Background()
	for name, manager := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			store := manager.(ConversationStore)
			state := testDemandState()
			result := json.RawMessage(`{"status":"intent_ready","_demand_request_sha256":"private-fingerprint"}`)
			conversation, turn, err := store.SaveTurn(ctx, SaveTurnReq{UserId: "u", ConversationId: "c", TurnId: "t", Query: "desk", Intent: state, ResultJSON: result})
			if err != nil {
				t.Fatal(err)
			}
			mutateDemandState(state)
			mutateDemandState(conversation.State)
			mutateDemandState(turn.Intent)
			result[0], turn.ResultJSON[0] = '!', '!'
			for range 2 {
				conversation, found, err := store.GetConversation(ctx, "u", "c")
				if err != nil || !found || !reflect.DeepEqual(conversation.State, testDemandState()) {
					t.Fatalf("conversation alias: %+v %v", conversation, err)
				}
				turn, found, err := store.FindTurn(ctx, "u", "c", "t")
				if err != nil || !found || !reflect.DeepEqual(turn.Intent, testDemandState()) || !json.Valid(turn.ResultJSON) || !strings.Contains(string(turn.ResultJSON), "private-fingerprint") {
					t.Fatalf("turn alias/metadata loss: %+v %v", turn, err)
				}
				mutateDemandState(conversation.State)
				mutateDemandState(turn.Intent)
				list, _, err := store.ListConversations(ctx, "u", 1, 10)
				if err != nil || !reflect.DeepEqual(list[0].State, testDemandState()) {
					t.Fatalf("list alias: %+v %v", list, err)
				}
				mutateDemandState(list[0].State)
				_, turns, _, _, err := store.ListTurns(ctx, "u", "c", 1, 10)
				if err != nil || !reflect.DeepEqual(turns[0].Intent, testDemandState()) {
					t.Fatalf("turn list alias: %+v %v", turns, err)
				}
				mutateDemandState(turns[0].Intent)
			}
		})
	}
}

func TestDemandPostgresJSONAdaptersAcceptOldAndNewState(t *testing.T) {
	// Tests the actual JSONB adapters without connecting to a database.
	for _, want := range []IntentState{{BudgetCents: 10000, MaxItems: 3}, testDemandState()} {
		data, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		conversation, err := decodeConversation(conversation_memory.AgentConversation{State: data})
		if err != nil || !reflect.DeepEqual(conversation.State, want) {
			t.Fatalf("conversation JSONB: %+v %v", conversation, err)
		}
		turn, err := decodeTurn(conversation_memory.AgentConversationTurn{Intent: data, Result: []byte(`{"_demand_request_sha256":"private"}`)})
		if err != nil || !reflect.DeepEqual(turn.Intent, want) || !strings.Contains(string(turn.ResultJSON), "private") {
			t.Fatalf("turn JSONB: %+v %v", turn, err)
		}
		if want.Demand == nil && strings.Contains(string(data), `"demand"`) {
			t.Fatal("old JSON contract changed")
		}
	}
}
