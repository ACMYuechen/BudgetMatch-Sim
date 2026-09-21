package recommendservicelogic

import (
	"context"
	"encoding/json"
	"testing"

	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/interceptor"
	"budgetmatch-sim/services/rpc/agent/internal/agent/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/demandexec"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/svc"
	"budgetmatch-sim/services/rpc/agent/pb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExecuteDemandRPCAuthenticationMappingAndHistory(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	executor, err := demandexec.NewBuiltinDemo()
	require.NoError(t, err)
	svcCtx := &svc.ServiceContext{RecommendService: recommend.NewService(nil, nil, mem).WithDemandExecutor(executor)}
	req := &pb.ExecuteDemandReq{ConversationId: "c", PlanTurnId: "plan", TurnId: "execute"}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "spoofed")} {
		_, err := NewExecuteDemandLogic(ctx, svcCtx).ExecuteDemand(req)
		require.ErrorIs(t, err, apperrors.Unauthorized)
	}
	ctx := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "trusted")
	_, err = NewPlanDemandLogic(ctx, svcCtx).PlanDemand(&pb.PlanDemandReq{ConversationId: "c", TurnId: "plan", Query: "desk", BudgetCents: 40000,
		DemandPatch: `{"schema_version":1,"required":{"operation":"replace","values":["keyboard","mouse"]}}`})
	require.NoError(t, err)
	resp, err := NewExecuteDemandLogic(ctx, svcCtx).ExecuteDemand(req)
	require.NoError(t, err)
	require.Equal(t, "complete", resp.Status)
	require.Equal(t, "synthetic_demo_snapshot_only", resp.Execution.Scope)
	require.Equal(t, "plan", resp.Execution.PlanTurnId)
	require.Equal(t, int64(35800), resp.TotalPriceCents)
	turn, _, err := mem.FindTurn(ctx, "trusted", "c", "execute")
	require.NoError(t, err)
	history, err := toPBTurn(turn)
	require.NoError(t, err)
	require.Equal(t, resp.Execution, history.Result.Execution)
	encoded, err := json.Marshal(history)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "_demand_")
	require.NotContains(t, string(encoded), "user_id")
	other := context.WithValue(context.Background(), interceptor.ContextKeyUserId, "other")
	_, err = NewExecuteDemandLogic(other, svcCtx).ExecuteDemand(req)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = NewExecuteDemandLogic(ctx, svcCtx).ExecuteDemand(nil)
	require.ErrorIs(t, err, apperrors.Invalid)
	req.PlanTurnId = "different"
	_, err = NewExecuteDemandLogic(ctx, svcCtx).ExecuteDemand(req)
	require.ErrorIs(t, err, apperrors.AgentTurnConflict)
	_, err = (pb.UnimplementedRecommendServiceServer{}).ExecuteDemand(ctx, req)
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
