package recommendservicelogic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPlanDemandRPCAuthenticationAndIsolation(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	svcCtx := &svc.ServiceContext{RecommendService: recommend.NewService(nil, nil, mem)}
	req := &pb.PlanDemandReq{Query: "desk", ConversationId: "c", TurnId: "t", DemandPatch: `{"schema_version":1,"required":{"operation":"add","values":["keyboard"]}}`}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "spoofed")} {
		if _, err := NewPlanDemandLogic(ctx, svcCtx).PlanDemand(req); !errors.Is(err, apperrors.Unauthorized) {
			t.Fatalf("unauthenticated call: %v", err)
		}
	}
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "trusted")
	r, err := NewPlanDemandLogic(ctx, svcCtx).PlanDemand(req)
	if err != nil || r.Status != "intent_ready" || r.Intent.Demand.Required[0] != "keyboard" {
		t.Fatalf("planning RPC: %+v %v", r, err)
	}
	if _, found, _ := mem.GetConversation(ctx, "spoofed", "c"); found {
		t.Fatal("spoofed ownership")
	}
	_, turns, _, _, err := mem.ListTurns(ctx, "trusted", "c", 1, 10)
	if err != nil || len(turns) != 1 {
		t.Fatal(err)
	}
	history, err := toPBTurn(turns[0])
	if err != nil || history.Result.Status != "intent_ready" || history.Intent.Demand == nil || history.Result.Intent.Demand == nil {
		t.Fatalf("history lost planning state: %+v %v", history, err)
	}
	encoded, _ := json.Marshal(history)
	if strings.Contains(string(encoded), "sha256") || strings.Contains(string(encoded), "_demand_planning") {
		t.Fatal("private fingerprint exposed")
	}
	if toPBConversation(memory.Conversation{State: turns[0].Intent}).State.Demand == nil {
		t.Fatal("summary lost demand")
	}
	req.TurnId, req.MaxItems = "conflict", 1
	req.DemandPatch = `{"schema_version":1,"required":{"operation":"add","values":["mouse"]}}`
	r, err = NewPlanDemandLogic(ctx, svcCtx).PlanDemand(req)
	if err != nil || r.Status != "needs_clarification" || len(r.Conflicts) != 1 || r.Intent.MaxItems != 3 {
		t.Fatalf("clarification mapping: %+v %v", r, err)
	}
}

func TestPlanDemandRPCErrorsAndOldServer(t *testing.T) {
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "trusted")
	svcCtx := &svc.ServiceContext{RecommendService: recommend.NewService(nil, nil, memory.NewInMemory(memory.Conf{}))}
	if _, err := NewPlanDemandLogic(ctx, svcCtx).PlanDemand(&pb.PlanDemandReq{Query: "desk", DemandPatch: `{"private":"SECRET"}`}); !errors.Is(err, apperrors.Invalid) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe invalid error: %v", err)
	}
	if _, err := NewPlanDemandLogic(ctx, svcCtx).PlanDemand(nil); !errors.Is(err, apperrors.Invalid) {
		t.Fatalf("nil input: %v", err)
	}
	if status.Code(mapRecommendError(agent.ErrDemandNotExecutable)) != codes.FailedPrecondition {
		t.Fatal("execution guard code changed")
	}
	if _, err := (pb.UnimplementedRecommendServiceServer{}).PlanDemand(ctx, &pb.PlanDemandReq{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("old implementation accepted planning")
	}
}
