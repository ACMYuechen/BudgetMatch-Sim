package recommend

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/internal/runtrace"
	"github.com/stretchr/testify/require"
)

type budgetKey struct{}
type budgetAgent struct {
	run func(context.Context) (*agentcore.Result, error)
}

func (*budgetAgent) Name() string { return "test.budget" }
func (a *budgetAgent) Run(ctx context.Context, _ agentcore.Input) (*agentcore.Result, error) {
	return a.run(ctx)
}

type budgetFinalizer struct{ run func(context.Context) error }

func (f budgetFinalizer) Finalize(ctx context.Context, _ *agentcore.Result) error { return f.run(ctx) }

type budgetStore struct {
	*memory.InMemory
	save func(context.Context, memory.SaveTurnReq) (memory.Conversation, memory.Turn, error)
}

func (s *budgetStore) WithConversationLock(ctx context.Context, user, conversation string, fn func(context.Context) error) error {
	return s.InMemory.WithConversationLock(ctx, user, conversation, func(locked context.Context) error {
		return fn(context.WithValue(locked, budgetKey{}, "locked connection"))
	})
}
func (s *budgetStore) SaveTurn(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	return s.save(ctx, req)
}

func TestStreamServiceReservesCompletionAndPreservesLockedValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, trace := runtrace.Start(ctx, "execution")
	parentEnd, _ := ctx.Deadline()
	var genContext context.Context
	var generationEnd, finalEnd time.Time
	runner := &budgetAgent{run: func(ctx context.Context) (*agentcore.Result, error) {
		require.Equal(t, "locked connection", ctx.Value(budgetKey{}))
		genContext = ctx
		generationEnd, _ = ctx.Deadline()
		return &agentcore.Result{Summary: "candidate"}, nil
	}}
	store := &budgetStore{InMemory: memory.NewInMemory(memory.Conf{})}
	store.save = func(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
		require.Equal(t, "locked connection", ctx.Value(budgetKey{}))
		require.NoError(t, ctx.Err())
		end, _ := ctx.Deadline()
		require.Equal(t, finalEnd, end)
		return store.InMemory.SaveTurn(ctx, req)
	}
	finalizer := budgetFinalizer{run: func(ctx context.Context) error {
		require.ErrorIs(t, genContext.Err(), context.Canceled, "completed generation is cleaned up before finalization")
		require.NoError(t, ctx.Err())
		require.Equal(t, "locked connection", ctx.Value(budgetKey{}))
		finalEnd, _ = ctx.Deadline()
		require.True(t, generationEnd.Before(finalEnd) && finalEnd.Before(parentEnd))
		return nil
	}}
	service := NewService(runner, nil, store).WithFinalizer(finalizer)
	in := agentcore.Input{Query: "desk", UserId: "u", ConversationId: "c", TurnId: "t"}
	_, err := service.RecommendStream(ctx, in, func(context.Context, string, string) error { return nil })
	require.NoError(t, err)
	require.True(t, trace.Snapshot().Committed)
	// Replay takes the saved path and does not need generation/finalizer time.
	replayContext, replay := runtrace.Start(context.Background(), "replay")
	_, err = service.RecommendStream(replayContext, in, func(context.Context, string, string) error {
		t.Fatal("replay cannot be accepted as a new turn")
		return nil
	})
	require.NoError(t, err)
	require.True(t, replay.Snapshot().Replayed)
	require.False(t, replay.Snapshot().Committed)
}

func TestStreamServiceBudgetExpiryNeverSavesOrFallbacks(t *testing.T) {
	for _, stage := range []string{"generation", "finalization", "save failure", "committed cancellation"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			defer cancel()
			ctx, trace := runtrace.Start(ctx, "execution")
			fallbacks, finalizations, saves := 0, 0, 0
			runner := &budgetAgent{run: func(ctx context.Context) (*agentcore.Result, error) {
				if stage == "generation" {
					<-ctx.Done()
				}
				return &agentcore.Result{Summary: "late candidate"}, nil
			}}
			fallback := &budgetAgent{run: func(context.Context) (*agentcore.Result, error) { fallbacks++; return &agentcore.Result{}, nil }}
			store := &budgetStore{InMemory: memory.NewInMemory(memory.Conf{})}
			store.save = func(ctx context.Context, req memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
				saves++
				if stage == "save failure" {
					return memory.Conversation{}, memory.Turn{}, errors.New("PRIVATE save failure")
				}
				c, turn, err := store.InMemory.SaveTurn(ctx, req)
				cancel()
				return c, turn, err
			}
			finalizer := budgetFinalizer{run: func(ctx context.Context) error {
				finalizations++
				if stage == "finalization" {
					<-ctx.Done()
				}
				return nil
			}}
			service := NewService(fallback, runner, store).WithFinalizer(finalizer)
			_, err := service.RecommendStream(ctx, agentcore.Input{Query: "desk", UserId: "u", ConversationId: "c", TurnId: "t"}, func(context.Context, string, string) error { return nil })
			require.Error(t, err)
			require.Zero(t, fallbacks)
			if stage == "generation" {
				require.Zero(t, finalizations)
			}
			if stage == "generation" || stage == "finalization" {
				require.Zero(t, saves)
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			require.Equal(t, stage == "committed cancellation", trace.Snapshot().Committed)
			_, saved, findErr := store.FindTurn(context.Background(), "u", "c", "t")
			require.NoError(t, findErr)
			require.Equal(t, stage == "committed cancellation", saved)
		})
	}
}
