package recommend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/demand"
	"budgetmatch-sim/services/rpc/agent/internal/demandexec"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/recommend/beam"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type executionSource struct {
	searches, checks atomic.Int32
	check            func(context.Context, []agent.ProductCandidate) (demandexec.Batch, error)
}

func (s *executionSource) Search(context.Context, agent.Intent) ([]agent.ProductCandidate, error) {
	s.searches.Add(1)
	var out []agent.ProductCandidate
	for _, id := range []string{"keyboard", "mouse"} {
		out = append(out, agent.ProductCandidate{Id: id, Name: id, PriceCents: 100, Stock: 1,
			Evidence: agent.CandidateEvidence{Source: agent.RetrievalDemo, State: agent.VerificationDemo, ProductID: "p-" + id}})
	}
	return out, nil
}
func (s *executionSource) Recheck(ctx context.Context, c []agent.ProductCandidate) (demandexec.Batch, error) {
	s.checks.Add(1)
	if s.check != nil {
		return s.check(ctx, c)
	}
	batch := demandexec.Batch{Candidates: c, CheckedAtMs: time.Now().UnixMilli()}
	for i := range c {
		c[i].Evidence.VerifiedAtUnixMs = batch.CheckedAtMs
	}
	return batch, nil
}

func executionFixture(t *testing.T, mem memory.Manager) (*Service, *executionSource) {
	t.Helper()
	source := &executionSource{}
	products, _ := source.Search(context.Background(), agent.Intent{})
	source.searches.Store(0)
	data, _ := json.Marshal(products)
	binding := demand.DatasetBinding{Version: "service_test_v1", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	encoded, err := json.Marshal(struct {
		demand.CatalogMetadata
		Entries []demand.CategoryEntry `json:"entries"`
	}{
		demand.CatalogMetadata{Version: "service_categories_v1", TaxonomyVersion: demand.TaxonomyVersion,
			Provenance: "synthetic_demo", AnnotationStatus: "pending_human_review", DatasetBinding: binding},
		[]demand.CategoryEntry{{SKUID: "keyboard", ProductID: "p-keyboard", Category: demand.Keyboard}, {SKUID: "mouse", ProductID: "p-mouse", Category: demand.Mouse}}})
	require.NoError(t, err)
	catalog, err := demand.LoadDemoCatalog(bytes.NewReader(encoded), binding)
	require.NoError(t, err)
	executor, err := demandexec.New(source, catalog, beam.Config{})
	require.NoError(t, err)
	legacy := agentFunc(func(context.Context, agent.Input) (*agent.Result, error) {
		t.Error("legacy runner executed")
		return nil, agent.ErrUnsafeResult
	})
	return NewService(legacy, legacy, mem).WithDemandExecutor(executor).WithFinalizer(demandFinalizerFunc(func(context.Context, *agent.Result) error {
		t.Error("legacy finalizer executed")
		return agent.ErrUnsafeResult
	})), source
}

func readyPlan(t *testing.T, s *Service, turnID string) *agent.Result {
	t.Helper()
	result, err := s.PlanDemand(context.Background(), agent.Input{UserId: "u", ConversationId: "c", TurnId: turnID,
		Query: "desk", BudgetCents: 1000, MaxItems: 3}, `{"schema_version":1,"required":{"operation":"replace","values":["keyboard","mouse"]},"preferences":{"operation":"replace","values":["quiet"]}}`)
	require.NoError(t, err)
	require.Equal(t, "intent_ready", result.Status)
	return result
}

func publicResult(t *testing.T, result *agent.Result) string {
	t.Helper()
	data, err := json.Marshal(result)
	require.NoError(t, err)
	return string(data)
}

func TestDemandExecutionPersistsAndReplaysBeforePlanOrModeChecks(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{})
	s, source := executionFixture(t, mem)
	plan := readyPlan(t, s, "plan-1")
	ctx := context.Background()
	first, err := s.ExecuteDemand(ctx, "u", "c", "plan-1", "execute-1")
	require.NoError(t, err)
	require.Equal(t, beam.Complete, first.Status)
	require.Equal(t, plan.Intent, first.Intent)
	require.Equal(t, "plan-1", first.Execution.PlanTurnID)
	require.Equal(t, int64(200), first.TotalPriceCents)
	conversation, _, _ := mem.GetConversation(ctx, "u", "c")
	require.True(t, conversation.State.PlanningOnly)
	require.True(t, conversation.State.DemandPlanReady)
	require.Equal(t, "plan-1", conversation.State.DemandPlanTurnID)
	require.Equal(t, int64(2), conversation.TurnCount)
	_, err = s.Recommend(ctx, agent.Input{UserId: "u", ConversationId: "c", TurnId: "bypass", Query: "desk"})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	readyPlan(t, s, "plan-2")
	s.demandExecutor = nil // An old completed request remains replayable when disabled.
	replay, err := s.ExecuteDemand(ctx, "u", "c", "plan-1", "execute-1")
	require.NoError(t, err)
	require.JSONEq(t, publicResult(t, first), publicResult(t, replay))
	require.Equal(t, int32(1), source.searches.Load())
	require.Equal(t, int32(1), source.checks.Load())
	_, err = s.ExecuteDemand(ctx, "u", "c", "plan-2", "execute-1")
	require.ErrorIs(t, err, agent.ErrTurnConflict)
	_, err = s.ExecuteDemand(ctx, "u", "c", "plan-2", "execute-2")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = s.ExecuteDemand(ctx, "other-user", "c", "plan-2", "execute-1")
	require.Equal(t, codes.NotFound, status.Code(err))
	turn, _, _ := mem.FindTurn(ctx, "u", "c", "execute-1")
	require.Contains(t, string(turn.ResultJSON), "_demand_request_sha256")
	require.NotContains(t, publicResult(t, replay), "_demand_")
}

func TestStaleConflictAndLegacyPlansCannotExecute(t *testing.T) {
	ctx := context.Background()
	mem := memory.NewInMemory(memory.Conf{})
	s, source := executionFixture(t, mem)
	readyPlan(t, s, "plan-1")
	readyPlan(t, s, "plan-2")
	_, err := s.ExecuteDemand(ctx, "u", "c", "plan-1", "stale")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	conflict, err := s.PlanDemand(ctx, agent.Input{UserId: "u", ConversationId: "c", TurnId: "conflict", Query: "desk"},
		`{"schema_version":1,"excluded":{"operation":"add","values":["keyboard"]}}`)
	require.NoError(t, err)
	require.Equal(t, "needs_clarification", conflict.Status)
	for _, planID := range []string{"plan-2", "conflict"} {
		_, err = s.ExecuteDemand(ctx, "u", "c", planID, "blocked")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	}
	require.Zero(t, source.searches.Load())
	conversation, _, _ := mem.GetConversation(ctx, "u", "c")
	state := conversation.State
	state.DemandPlanReady, state.DemandPlanTurnID = false, ""
	_, _, err = mem.SaveTurn(ctx, memory.SaveTurnReq{UserId: "u", ConversationId: "old", TurnId: "old-plan", Query: "desk", Intent: state, ResultJSON: []byte(`{"status":"intent_ready"}`)})
	require.NoError(t, err)
	_, err = s.ExecuteDemand(ctx, "u", "old", "old-plan", "blocked")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = s.ExecuteDemand(ctx, "u", "c", "plan-2", "plan-2")
	require.ErrorIs(t, err, agent.ErrTurnConflict, "planning turn IDs cannot be reused for execution")
}

func TestConcurrentDemandExecutionRunsOnceAndRetainsWithdrawals(t *testing.T) {
	mem := memory.NewInMemory(memory.Conf{MaxHistory: 2})
	s, source := executionFixture(t, mem)
	readyPlan(t, s, "plan")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ExecuteDemand(context.Background(), "u", "c", "plan", "same")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), source.searches.Load())
	require.Equal(t, int32(1), source.checks.Load())
	input := agent.Input{UserId: "u", ConversationId: "c", TurnId: "withdraw", Query: "quiet keyboard"}
	_, err := s.PlanDemand(context.Background(), input, `{"schema_version":1,"required":{"operation":"remove","values":["keyboard"]},"preferences":{"operation":"replace","values":[]}}`)
	require.NoError(t, err)
	out, err := s.ExecuteDemand(context.Background(), "u", "c", "withdraw", "new-execute")
	require.NoError(t, err)
	require.Equal(t, []string{"mouse"}, out.Intent.Demand.Required)
	require.Empty(t, out.Intent.Preferences)
	require.Empty(t, out.Execution.UnscoredPreferences)
	require.Len(t, out.Items, 1)
	require.Equal(t, "mouse", out.Items[0].Id)
}

type executionFailStore struct{ memory.ConversationStore }

func (s executionFailStore) SaveTurn(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	return memory.Conversation{}, memory.Turn{}, errors.New("PRIVATE database failure")
}

func TestExecutionFailuresDoNotSaveOrFallBack(t *testing.T) {
	for _, phase := range []string{"check_failure", "incomplete", "canceled", "save_failure"} {
		t.Run(phase, func(t *testing.T) {
			mem := memory.NewInMemory(memory.Conf{})
			s, source := executionFixture(t, mem)
			readyPlan(t, s, "plan")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "save_failure" {
				s.memory = executionFailStore{mem}
			} else {
				source.check = func(context.Context, []agent.ProductCandidate) (demandexec.Batch, error) {
					if phase == "canceled" {
						cancel()
					}
					if phase == "check_failure" {
						return demandexec.Batch{}, errors.New("PRIVATE provider failure")
					}
					return demandexec.Batch{CheckedAtMs: time.Now().UnixMilli()}, nil
				}
			}
			out, err := s.ExecuteDemand(ctx, "u", "c", "plan", "failed")
			require.Error(t, err)
			require.Nil(t, out)
			require.NotContains(t, err.Error(), "PRIVATE")
			_, found, err := mem.FindTurn(context.Background(), "u", "c", "failed")
			require.NoError(t, err)
			require.False(t, found)
			conversation, _, _ := mem.GetConversation(context.Background(), "u", "c")
			require.Equal(t, int64(1), conversation.TurnCount)
		})
	}
}

func TestExecuteValidationAndNoFeasibleReplay(t *testing.T) {
	ctx := context.Background()
	mem := memory.NewInMemory(memory.Conf{})
	s, source := executionFixture(t, mem)
	for _, args := range [][4]string{{"", "c", "p", "t"}, {"u", "", "p", "t"}, {"u", "c", " p", "t"}, {"u", "c", "p", strings.Repeat("x", 129)}} {
		_, err := s.ExecuteDemand(ctx, args[0], args[1], args[2], args[3])
		require.ErrorIs(t, err, agent.ErrInvalidInput)
	}
	readyPlan(t, s, "plan")
	source.check = func(_ context.Context, c []agent.ProductCandidate) (demandexec.Batch, error) {
		batch := demandexec.Batch{CheckedAtMs: time.Now().UnixMilli()}
		for _, p := range c {
			batch.Unavailable = append(batch.Unavailable, p.Id)
		}
		return batch, nil
	}
	out, err := s.ExecuteDemand(ctx, "u", "c", "plan", "")
	require.NoError(t, err)
	require.NotEmpty(t, out.TurnId)
	require.Equal(t, beam.NoFeasibleBundle, out.Status)
	require.Empty(t, out.Items)
	replayed, err := s.ExecuteDemand(ctx, "u", "c", "plan", out.TurnId)
	require.NoError(t, err)
	require.JSONEq(t, publicResult(t, out), publicResult(t, replayed))
	require.Equal(t, int32(1), source.checks.Load())
}
