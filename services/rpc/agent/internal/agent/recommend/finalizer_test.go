package recommend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	selector "budgetmatch-sim/services/rpc/agent/internal/recommend"
	"budgetmatch-sim/services/rpc/agent/internal/tools"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type finalCheckClient struct {
	calls  int
	ids    []string
	prices map[string]int64
	err    error
}

func (c *finalCheckClient) CheckProductCandidates(_ context.Context, req *pb.CheckProductCandidatesReq, _ ...grpc.CallOption) (*pb.CheckProductCandidatesResp, error) {
	c.calls++
	c.ids = append([]string(nil), req.SkuIds...)
	if c.err != nil {
		return nil, c.err
	}
	resp := &pb.CheckProductCandidatesResp{CheckedAtUnixMs: time.Now().UnixMilli()}
	for _, id := range req.SkuIds {
		price := c.prices[id]
		check := &pb.CandidateCheck{SkuId: id, State: pb.CandidateState_CANDIDATE_STATE_UNAVAILABLE}
		if price > 0 {
			check.State = pb.CandidateState_CANDIDATE_STATE_ACTIVE
			check.Facts = &pb.CandidateFacts{SkuId: id, ProductId: "p", ProductName: id, Price: price, Stock: 2}
		}
		resp.Results = append(resp.Results, check)
	}
	return resp, nil
}
func finalCandidate(id string, price int64) agentcore.ProductCandidate {
	return agentcore.ProductCandidate{Id: id, Name: id, PriceCents: price, Stock: 2, Source: "mall",
		Evidence: agentcore.CandidateEvidence{Source: agentcore.RetrievalMallKeyword, ProductID: "p"}}
}
func finalResult(candidates ...agentcore.ProductCandidate) *agentcore.Result {
	intent := agentcore.Intent{BudgetCents: 1000, MaxItems: 2}
	items, total := selector.NewBundleSelector().Select(candidates, intent)
	return &agentcore.Result{Candidates: candidates, Intent: intent, Items: items, TotalPriceCents: total}
}
func strictFinalizer(client *finalCheckClient) *CandidateFinalizer {
	return NewCandidateFinalizer(tools.NewMallCandidateVerifier(client))
}

func TestFinalizerRepricesAndReselectsWithinBudget(t *testing.T) {
	result := finalResult(finalCandidate("a", 100), finalCandidate("b", 200), finalCandidate("c", 300))
	require.Equal(t, "a", result.Items[0].Id)
	client := &finalCheckClient{prices: map[string]int64{"a": 1500, "b": 400, "c": 500}}
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Equal(t, 1, client.calls)
	require.Equal(t, []string{"a", "b", "c"}, client.ids)
	require.EqualValues(t, 900, result.TotalPriceCents)
	require.Len(t, result.Items, 2)
	require.Equal(t, "b", result.Items[0].Id)
	require.Equal(t, "c", result.Items[1].Id)
	require.Contains(t, result.Summary, "核验时点")
	require.Contains(t, result.Summary, "9.00 元")
	require.Equal(t, "candidate.verify", result.ToolsUsed[0].Name)
}

func TestFinalizerKeepsTighterLimitsAndExplicitScope(t *testing.T) {
	result := finalResult(finalCandidate("a", 100), finalCandidate("b", 200), finalCandidate("c", 50))
	result.Selection = &agentcore.SelectionScope{CandidateIDs: []string{"a", "b"}, Limits: agentcore.Constraints{BudgetCents: 500, MaxItems: 1}}
	result.Items, result.TotalPriceCents = selector.NewBundleSelector().Select(result.Candidates[:2], agentcore.Intent{BudgetCents: 500, MaxItems: 1})
	client := &finalCheckClient{prices: map[string]int64{"a": 600, "b": 400, "c": 1}}
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Equal(t, []string{"a", "b"}, client.ids)
	require.Len(t, result.Items, 1)
	require.Equal(t, "b", result.Items[0].Id)
	require.EqualValues(t, 400, result.TotalPriceCents)
	client.prices["b"] = 550
	result.Items = nil
	result.TotalPriceCents = 0
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Empty(t, result.Items, "must not widen the 500-cent cap or use c")
}

func TestFinalizerShortlistIsBoundedAndKeepsSelectedIDs(t *testing.T) {
	result := finalResult()
	client := &finalCheckClient{prices: map[string]int64{}}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("sku-%03d", i)
		result.Candidates = append(result.Candidates, finalCandidate(id, int64(i+1)))
		client.prices[id] = int64(i + 1)
	}
	result.Items, result.TotalPriceCents = selector.NewBundleSelector().Select(result.Candidates[99:], result.Intent)
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Len(t, client.ids, candidatecontract.MaxCandidates)
	require.Equal(t, "sku-099", client.ids[0])
	require.Equal(t, 1, client.calls)
	for _, item := range result.Items {
		require.Contains(t, client.ids, item.Id)
	}
}

func TestFinalizerConfirmedAbsenceIsEmptyButNoCandidatesDoNotClaimVerification(t *testing.T) {
	client := &finalCheckClient{}
	result := finalResult(finalCandidate("a", 100))
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Empty(t, result.Items)
	require.Zero(t, result.TotalPriceCents)
	require.Contains(t, result.Summary, "核验于")
	result = finalResult()
	require.NoError(t, strictFinalizer(client).Finalize(context.Background(), result))
	require.Equal(t, 1, client.calls)
	require.NotContains(t, result.Summary, "核验")
}

func TestServiceFinalCheckCoversRulePrimaryAndFallback(t *testing.T) {
	for _, route := range []string{"rule", "primary", "fallback"} {
		t.Run(route, func(t *testing.T) {
			fallback := &stubAgent{name: "rule", result: finalResult(finalCandidate("a", 100))}
			var primary agentcore.Agent
			if route == "primary" {
				primary = &stubAgent{name: "primary", result: finalResult(finalCandidate("a", 100))}
			}
			if route == "fallback" {
				primary = &stubAgent{name: "primary", err: errors.New("model unavailable")}
			}
			client := &finalCheckClient{prices: map[string]int64{"a": 300}}
			service := NewService(fallback, primary, nil).WithFinalizer(strictFinalizer(client))
			result, err := service.Recommend(context.Background(), agentcore.Input{Query: "keyboard", BudgetCents: 1000, MaxItems: 2})
			require.NoError(t, err)
			require.Equal(t, 1, client.calls)
			require.EqualValues(t, 300, result.TotalPriceCents)
			require.Equal(t, "candidate.verify", result.ToolsUsed[len(result.ToolsUsed)-1].Name)
		})
	}
}

func TestServiceVerificationFailureNeverFallsBackOrSavesTurn(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.Unimplemented, codes.PermissionDenied, codes.Unauthenticated, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			mem := memory.NewInMemory(memory.Conf{})
			fallback := &countingAgent{}
			primary := &stubAgent{name: "primary", result: finalResult(finalCandidate("a", 100))}
			client := &finalCheckClient{err: status.Error(code, "secret-upstream")}
			service := NewService(fallback, primary, mem).WithFinalizer(strictFinalizer(client))
			input := agentcore.Input{Query: "keyboard", BudgetCents: 1000, MaxItems: 2, UserId: "u", ConversationId: "c", TurnId: "t"}
			result, err := service.Recommend(context.Background(), input)
			require.Error(t, err)
			require.Nil(t, result)
			require.Zero(t, fallback.calls)
			require.Equal(t, 1, client.calls)
			require.NotContains(t, err.Error(), "secret-upstream")
			_, found, readErr := mem.FindTurn(context.Background(), "u", "c", "t")
			require.NoError(t, readErr)
			require.False(t, found)
			client.err = nil
			client.prices = map[string]int64{"a": 250}
			result, err = service.Recommend(context.Background(), input)
			require.NoError(t, err)
			require.EqualValues(t, 250, result.TotalPriceCents)
		})
	}
}

func TestServiceReplayKeepsOriginalCheckAndPriceWithoutRPC(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	primary := &stubAgent{name: "primary", result: finalResult(finalCandidate("a", 100))}
	client := &finalCheckClient{prices: map[string]int64{"a": 250}}
	service := NewService(primary, nil, mem).WithFinalizer(strictFinalizer(client))
	input := agentcore.Input{Query: "keyboard", BudgetCents: 1000, MaxItems: 2, UserId: "u", ConversationId: "c", TurnId: "t"}
	first, err := service.Recommend(context.Background(), input)
	require.NoError(t, err)
	client.err = status.Error(codes.Unavailable, "offline after successful check")
	replay, err := service.Recommend(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 1, client.calls)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	replayJSON, err := json.Marshal(replay)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(replayJSON))
	require.NotContains(t, string(firstJSON), "Evidence")
	require.Empty(t, replay.Candidates)
	require.EqualValues(t, 250, replay.TotalPriceCents)
}

func TestDemoPolicyIsExplicitAndStrictMallRejectsDemo(t *testing.T) {
	rule := NewAgent(tools.NewMockProductProvider(), selector.NewBundleSelector())
	input := agentcore.Input{Query: "study", BudgetCents: 100000, MaxItems: 2}
	result, err := NewService(rule, nil, nil).WithFinalizer(DemoFinalizer{}).Recommend(context.Background(), input)
	require.NoError(t, err)
	require.Contains(t, result.Summary, "演示数据，非实时商城库存")
	client := &finalCheckClient{}
	_, err = NewService(rule, nil, nil).WithFinalizer(strictFinalizer(client)).Recommend(context.Background(), input)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Zero(t, client.calls)
	result = finalResult(finalCandidate("a", 100))
	require.ErrorIs(t, (DemoFinalizer{}).Finalize(context.Background(), result), agentcore.ErrUnsafeResult)
}

func TestFinalizerRejectsInvalidSelectionBeforeRPC(t *testing.T) {
	for _, scope := range []*agentcore.SelectionScope{
		{CandidateIDs: []string{"a"}, Limits: agentcore.Constraints{BudgetCents: 1001, MaxItems: 1}},
		{CandidateIDs: []string{"a"}, Limits: agentcore.Constraints{BudgetCents: 1000, MaxItems: 3}},
		{CandidateIDs: []string{"a"}, Limits: agentcore.Constraints{BudgetCents: 0, MaxItems: 1}},
		{CandidateIDs: []string{"b"}, Limits: agentcore.Constraints{BudgetCents: 1000, MaxItems: 1}},
		{CandidateIDs: []string{"a"}, Limits: agentcore.Constraints{BudgetCents: 50, MaxItems: 1}},
	} {
		result := finalResult(finalCandidate("a", 100))
		result.Selection = scope
		client := &finalCheckClient{}
		require.ErrorIs(t, strictFinalizer(client).Finalize(context.Background(), result), agentcore.ErrUnsafeResult)
		require.Zero(t, client.calls)
	}
}
