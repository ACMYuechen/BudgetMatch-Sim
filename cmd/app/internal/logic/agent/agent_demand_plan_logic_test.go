package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"
	"google.golang.org/grpc"
)

type demandClient struct {
	recommendservice.RecommendService
	calls int
	req   *recommendservice.PlanDemandReq
}

func (c *demandClient) PlanDemand(_ context.Context, req *recommendservice.PlanDemandReq, _ ...grpc.CallOption) (*recommendservice.PlanDemandResp, error) {
	c.calls++
	c.req = req
	return &recommendservice.PlanDemandResp{Status: "needs_clarification", ConversationId: "c", TurnId: "t",
		Intent:    &recommendservice.Intent{BudgetCents: 500, MaxItems: 2, Preferences: []string{"quiet"}, Demand: &recommendservice.DemandState{SchemaVersion: 1, Required: []string{"keyboard"}}},
		Conflicts: []*recommendservice.DemandConflict{{Code: "required_and_excluded", Category: "keyboard"}}}, nil
}

func TestDemandPlanGatewayAuthenticationAndMapping(t *testing.T) {
	client := &demandClient{}
	svcCtx := &svc.ServiceContext{AgentClient: client}
	req := &types.AgentDemandPlanReq{Query: "desk", DemandPatch: `{"schema_version":1}`, BudgetCents: 500, MaxItems: 2, ConversationId: "c", TurnId: "t"}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "spoofed")} {
		if _, err := NewAgentDemandPlanLogic(ctx, svcCtx).AgentDemandPlan(req); !errors.Is(err, apperrors.Unauthorized) || client.calls != 0 {
			t.Fatalf("missing auth forwarded: %v", err)
		}
	}
	ctx := request.WithUserId(context.Background(), "trusted")
	r, err := NewAgentDemandPlanLogic(ctx, svcCtx).AgentDemandPlan(req)
	if err != nil || r.Status != "needs_clarification" || len(r.Conflicts) != 1 || r.Intent.Demand.Required[0] != "keyboard" || client.calls != 1 {
		t.Fatalf("mapped response: %+v %v", r, err)
	}
	if client.req.DemandPatch != req.DemandPatch || client.req.BudgetCents != 500 || client.req.MaxItems != 2 || client.req.ConversationId != "c" || client.req.TurnId != "t" {
		t.Fatalf("changed request: %+v", client.req)
	}
	data, _ := json.Marshal(r)
	for _, forbidden := range []string{"items", "total_price_cents", "tools_used", "sha256", "user_id"} {
		if strings.Contains(string(data), `"`+forbidden+`"`) {
			t.Fatalf("planning response exposes %s", forbidden)
		}
	}
	if !strings.Contains(string(data), `"optional":[]`) {
		t.Fatalf("empty demand array became null: %s", data)
	}
}

func TestDemandHistoryGatewayAndLegacyJSON(t *testing.T) {
	intent := &recommendservice.Intent{Demand: &recommendservice.DemandState{SchemaVersion: 1, Required: []string{"keyboard"}}}
	turn := mapConversationTurn(&recommendservice.ConversationTurn{Intent: intent, Result: &recommendservice.RecommendResp{Intent: intent, Status: "needs_clarification", DemandConflicts: []*recommendservice.DemandConflict{{Code: "required_and_excluded", Category: "keyboard"}}}})
	if turn.Intent.Demand == nil || turn.Result.Intent.Demand == nil || turn.Result.Status != "needs_clarification" || len(turn.Result.DemandConflicts) != 1 {
		t.Fatalf("history mapping: %+v", turn)
	}
	summary := mapConversationSummary(&recommendservice.ConversationSummary{State: intent})
	summary.State.Demand.Required[0] = "mutated"
	if intent.Demand.Required[0] != "keyboard" {
		t.Fatal("mapper shares mutable demand")
	}
	old, _ := json.Marshal(mapRecommendResp(&recommendservice.RecommendResp{}))
	for _, field := range []string{"status", "demand", "demand_conflicts"} {
		if strings.Contains(string(old), `"`+field+`"`) {
			t.Fatalf("legacy JSON gained %s: %s", field, old)
		}
	}
}
