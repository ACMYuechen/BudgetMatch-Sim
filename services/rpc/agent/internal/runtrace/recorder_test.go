package runtrace

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/streamcontract"
	"github.com/stretchr/testify/require"
)

func TestStreamBudgetReservesAndPreservesContext(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct{ total, gen, complete time.Duration }{
		{30 * time.Second, 26750 * time.Millisecond, 29750 * time.Millisecond},
		{time.Second, 750 * time.Millisecond, 950 * time.Millisecond},
		{-time.Second, -time.Second, -time.Second},
	} {
		gen, complete := splitDeadline(now, now.Add(tc.total))
		require.Equal(t, now.Add(tc.gen), gen)
		require.Equal(t, now.Add(tc.complete), complete)
	}
	type key struct{}
	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), key{}, "store connection"), time.Second)
	defer cancel()
	completion, stop := StreamContext(parent)
	defer stop()
	generation, end := GenerationContext(completion)
	defer end()
	a, _ := generation.Deadline()
	b, _ := completion.Deadline()
	c, _ := parent.Deadline()
	require.True(t, a.Before(b) && b.Before(c))
	require.Equal(t, "store connection", generation.Value(key{}))
	cancel()
	require.ErrorIs(t, generation.Err(), context.Canceled)
	unary := context.Background()
	unchanged, noop := GenerationContext(unary)
	noop()
	require.Equal(t, unary, unchanged)
}

func TestUsageSnapshotsDoNotDoubleCountOrInventMissingUsage(t *testing.T) {
	for _, mode := range []string{"duplicate", "increasing", "missing", "negative", "inconsistent", "overflow", "decreasing", "error", "early close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, r := Start(context.Background(), "execution")
			require.Same(t, r, From(ctx))
			call := r.BeginModel(1000)
			if mode != "missing" {
				call.Observe(Usage{10, 2, 12})
			}
			switch mode {
			case "duplicate":
				call.Observe(Usage{10, 2, 12})
			case "increasing":
				call.Observe(Usage{10, 3, 13})
			case "negative":
				call.Observe(Usage{-1, 2, 1})
			case "inconsistent":
				call.Observe(Usage{10, 2, 99})
			case "overflow":
				call.Observe(Usage{1_000_000_001, 0, 1_000_000_001})
			case "decreasing":
				call.Observe(Usage{9, 2, 11})
			}
			if mode != "early close" {
				call.End(mode != "error")
			}
			got := r.Finish(nil, false)
			require.Equal(t, "unknown", got.CostStatus)
			require.Equal(t, int64(1000), got.EstimatedTokens)
			if mode == "duplicate" || mode == "increasing" {
				require.Equal(t, "complete", got.UsageStatus)
				require.Equal(t, int64(10), got.ReportedUsage.Prompt)
			} else {
				require.Equal(t, "unknown", got.UsageStatus)
				require.Nil(t, got.ReportedUsage)
				require.Equal(t, 1, got.UsageUnknownCalls)
			}
			call.Observe(Usage{100, 0, 100})
			call.End(true)
			require.Equal(t, got, r.Snapshot(), "late callbacks cannot rewrite a completed summary")
		})
	}
	_, mixed := Start(context.Background(), "mixed")
	known := mixed.BeginModel(20)
	known.Observe(Usage{10, 2, 12})
	known.End(true)
	mixed.BeginModel(20).End(false)
	partial := mixed.Finish(errors.New("PRIVATE"), false)
	require.Equal(t, "partial", partial.UsageStatus)
	require.Equal(t, 1, partial.UsageKnownCalls)
	require.Equal(t, int64(12), partial.ReportedUsage.Total)
	encoded, err := json.Marshal(partial)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE")
}

func TestRunTraceIsolationConcurrencyAndOutcome(t *testing.T) {
	_, r := Start(context.Background(), "first")
	_, other := Start(context.Background(), "second")
	r.Route("fallback", errors.New("PRIVATE cause"))
	r.Route("PRIVATE path", nil)
	r.Provider("PRIVATE provider")
	r.Sent(streamcontract.ToolStarted, 0)
	require.Nil(t, r.Snapshot().FirstDeltaMS)
	r.Sent(streamcontract.AnswerDelta, 6)
	var workers sync.WaitGroup
	for i := 0; i < streamcontract.MaxModelCalls; i++ {
		workers.Go(func() {
			call := r.BeginModel(100)
			call.Observe(Usage{10, 1, 11})
			call.Observe(Usage{10, 1, 11})
			call.End(true)
			_ = r.Snapshot()
		})
	}
	workers.Wait()
	require.Nil(t, r.BeginModel(100), "metadata cannot grow beyond the model-call bound")
	r.Commit()
	got := r.Finish(context.Canceled, true)
	require.Equal(t, "canceled", got.Outcome)
	require.Equal(t, "fallback", got.Path)
	require.Equal(t, "execution_failed", got.FallbackCode)
	require.NotContains(t, got.SelectedProvider, "PRIVATE")
	require.True(t, got.Committed, "delivery failure cannot hide a successful commit")
	require.Equal(t, int64(99), got.ReportedUsage.Total)
	require.NotNil(t, got.FirstDeltaMS)
	*got.FirstDeltaMS = 9999
	require.NotEqual(t, got.FirstDeltaMS, r.Snapshot().FirstDeltaMS)
	other.Replay()
	replayed := other.Finish(nil, false)
	require.Equal(t, "replayed", replayed.Outcome)
	require.Equal(t, "no_model_calls", replayed.UsageStatus)
	require.Nil(t, replayed.FirstDeltaMS)
	require.Zero(t, replayed.ModelCalls)
	require.False(t, replayed.Committed)
}
