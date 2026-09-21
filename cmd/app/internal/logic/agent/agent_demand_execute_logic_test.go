package agent

import (
	"context"
	"encoding/json"
	"testing"

	"budgetmatch-sim/cmd/app/internal/svc"
	"budgetmatch-sim/cmd/app/internal/types"
	apperrors "budgetmatch-sim/infra/errors"
	"budgetmatch-sim/infra/request"
	"budgetmatch-sim/services/rpc/agent/client/recommendservice"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type executeClient struct {
	recommendservice.RecommendService
	req   *recommendservice.ExecuteDemandReq
	resp  *recommendservice.RecommendResp
	calls int
}

func (c *executeClient) ExecuteDemand(_ context.Context, req *recommendservice.ExecuteDemandReq, _ ...grpc.CallOption) (*recommendservice.RecommendResp, error) {
	c.calls++
	c.req = req
	return c.resp, nil
}

func TestDemandExecuteGatewayIdentityAndCompleteMapping(t *testing.T) {
	details := &recommendservice.DemandExecution{PlanTurnId: "plan", Scope: "synthetic_demo_snapshot_only", Strategy: "demand_beam_v1",
		MappingVersion: "mapping-v1", MappingSha256: "digest", SnapshotCheckedAtMs: 123, CoveredRequired: []string{"keyboard"},
		CoveredOptional: []string{"mouse"}, UnscoredPreferences: []string{"quiet"}, SearchLimited: true, CandidateWindow: 4,
		InitialExpansions: 8, FinalExpansions: 9, InitialStopReason: "expansion_limit", FinalStopReason: "enumeration_finished"}
	client := &executeClient{resp: &recommendservice.RecommendResp{Status: "complete", Execution: details, ConversationId: "c", TurnId: "execute"}}
	svcCtx := &svc.ServiceContext{AgentClient: client}
	req := &types.AgentDemandExecuteReq{ConversationId: "c", PlanTurnId: "plan", TurnId: "execute"}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), "user_id", "spoofed")} {
		_, err := NewAgentDemandExecuteLogic(ctx, svcCtx).AgentDemandExecute(req)
		require.ErrorIs(t, err, apperrors.Unauthorized)
	}
	require.Zero(t, client.calls)
	ctx := request.WithUserId(context.Background(), "trusted")
	out, err := NewAgentDemandExecuteLogic(ctx, svcCtx).AgentDemandExecute(req)
	require.NoError(t, err)
	require.Equal(t, &recommendservice.ExecuteDemandReq{ConversationId: "c", PlanTurnId: "plan", TurnId: "execute"}, client.req)
	require.Equal(t, details.Scope, out.Execution.Scope)
	require.Equal(t, details.SnapshotCheckedAtMs, out.Execution.SnapshotCheckedAtMs)
	require.Equal(t, details.InitialStopReason, out.Execution.InitialStopReason)
	require.Equal(t, details.FinalExpansions, out.Execution.FinalExpansions)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"missing_required":[]`)
	require.NotContains(t, string(encoded), "_demand_")
	require.NotContains(t, string(encoded), "user_id")
	out.Execution.CoveredRequired[0] = "mutated"
	require.Equal(t, "keyboard", details.CoveredRequired[0])
	turn := mapConversationTurn(&recommendservice.ConversationTurn{Result: client.resp})
	require.Equal(t, "keyboard", turn.Result.Execution.CoveredRequired[0])
	old, _ := json.Marshal(mapRecommendResp(&recommendservice.RecommendResp{}))
	require.NotContains(t, string(old), `"execution"`)
	_, err = NewAgentDemandExecuteLogic(ctx, svcCtx).AgentDemandExecute(nil)
	require.ErrorIs(t, err, apperrors.Invalid)
}
