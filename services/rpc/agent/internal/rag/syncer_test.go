package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

type syncFunc func(context.Context) (SyncStats, error)

func (f syncFunc) Sync(ctx context.Context) (SyncStats, error) { return f(ctx) }

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker signal")
	}
}

func shutdownSyncer(t *testing.T, s *Syncer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))
}

func TestSyncerConcurrentStartStopIsIdempotent(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := NewSyncer(syncFunc(func(ctx context.Context) (SyncStats, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-ctx.Done()
		close(exited)
		return SyncStats{}, ctx.Err()
	}), Config{})
	t.Cleanup(func() { shutdownSyncer(t, s) })
	var starters sync.WaitGroup
	for range 20 {
		starters.Go(s.Start)
	}
	starters.Wait()
	awaitSignal(t, entered)
	var stoppers sync.WaitGroup
	for range 20 {
		stoppers.Go(s.Stop)
	}
	stoppers.Wait()
	awaitSignal(t, exited)
	s.Start() // A stopped syncer cannot be restarted.
	require.EqualValues(t, 1, calls.Load())
}

func TestSyncerStopBeforeStartDoesNotRun(t *testing.T) {
	var calls atomic.Int32
	s := NewSyncer(syncFunc(func(context.Context) (SyncStats, error) {
		calls.Add(1)
		return SyncStats{}, nil
	}), Config{})
	s.Stop()
	s.Start()
	s.Stop()
	awaitSignal(t, s.done)
	require.Zero(t, calls.Load())
}

func TestSyncerRoundDeadlineAndOneShot(t *testing.T) {
	result := make(chan error, 1)
	s := NewSyncer(syncFunc(func(ctx context.Context) (SyncStats, error) {
		if _, ok := ctx.Deadline(); !ok {
			result <- errors.New("deadline missing")
			return SyncStats{}, nil
		}
		<-ctx.Done()
		result <- ctx.Err()
		return SyncStats{}, ctx.Err()
	}), Config{SyncIntervalSeconds: -1})
	s.runTimeout = 20 * time.Millisecond
	s.Start()
	t.Cleanup(func() { shutdownSyncer(t, s) })
	awaitSignal(t, s.done)
	require.ErrorIs(t, <-result, context.DeadlineExceeded)
	s.Start()
	shutdownSyncer(t, s)
}

func TestSyncerBoundedShutdownWithUncooperativeDependency(t *testing.T) {
	for _, useStop := range []bool{false, true} {
		name := "Shutdown"
		if useStop {
			name = "Stop"
		}
		t.Run(name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			var releaseOnce sync.Once
			s := NewSyncer(syncFunc(func(ctx context.Context) (SyncStats, error) {
				if calls.Add(1) == 1 {
					close(entered)
				}
				<-release // Intentionally ignore context while blocked.
				return SyncStats{}, ctx.Err()
			}), Config{})
			s.interval = time.Millisecond
			s.stopTimeout = 20 * time.Millisecond
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); shutdownSyncer(t, s) })
			s.Start()
			awaitSignal(t, entered)
			if useStop {
				returned := make(chan struct{})
				go func() { s.Stop(); close(returned) }()
				awaitSignal(t, returned)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				require.ErrorIs(t, s.Shutdown(ctx), context.DeadlineExceeded)
			}
			select {
			case <-s.done:
				t.Fatal("bounded return must not pretend dependency has exited")
			default:
			}
			releaseOnce.Do(func() { close(release) })
			shutdownSyncer(t, s)
			require.EqualValues(t, 1, calls.Load(), "no subsequent run after canceled dependency returns")
		})
	}
}

func TestSyncerRetriesSequentiallyAfterFailure(t *testing.T) {
	second := make(chan struct{})
	var calls, active, maxActive atomic.Int32
	s := NewSyncer(syncFunc(func(ctx context.Context) (SyncStats, error) {
		n := active.Add(1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		defer active.Add(-1)
		if calls.Add(1) == 1 {
			return SyncStats{}, errors.New("transient failure")
		}
		close(second)
		<-ctx.Done()
		return SyncStats{}, ctx.Err()
	}), Config{})
	s.interval = time.Millisecond
	t.Cleanup(func() { shutdownSyncer(t, s) })
	s.Start()
	awaitSignal(t, second)
	shutdownSyncer(t, s)
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, maxActive.Load())
}

func TestSyncerLogsBatchMetadataWithoutRawErrors(t *testing.T) {
	var buf bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buf))
	t.Cleanup(func() { logx.SetWriter(previous) })
	const secret = "PRIVATE_CATALOG_OR_CREDENTIAL_MARKER"
	for _, err := range []error{nil, errors.New(secret), context.DeadlineExceeded, ErrSyncBusy} {
		s := NewSyncer(syncFunc(func(context.Context) (SyncStats, error) {
			return SyncStats{Loaded: 2}, err
		}), Config{})
		s.runOnce(context.Background())
	}
	require.NotContains(t, buf.String(), secret)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 8)
	ids := make(map[string]bool)
	for i := 0; i < len(lines); i += 2 {
		var start, end map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[i]), &start))
		require.NoError(t, json.Unmarshal([]byte(lines[i+1]), &end))
		id, ok := start["batch_id"].(string)
		require.True(t, ok)
		require.NotEmpty(t, id)
		require.False(t, ids[id])
		ids[id] = true
		require.Equal(t, id, end["batch_id"])
		require.Contains(t, start, "started_at")
		for _, key := range []string{"finished_at", "duration_ms", "loaded", "indexed", "refreshed", "pruned"} {
			require.Contains(t, end, key)
		}
	}
}

func TestSyncerPanicIsRedactedAndWorkerExits(t *testing.T) {
	var buf bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&buf))
	t.Cleanup(func() { logx.SetWriter(previous) })
	s := NewSyncer(syncFunc(func(context.Context) (SyncStats, error) { panic("PRIVATE_PANIC_MARKER") }), Config{})
	t.Cleanup(func() { shutdownSyncer(t, s) })
	s.Start()
	awaitSignal(t, s.done)
	shutdownSyncer(t, s)
	require.Contains(t, buf.String(), "worker stopped after panic")
	require.NotContains(t, buf.String(), "PRIVATE_PANIC_MARKER")
}

func TestSyncConfigDefaultsAndBounds(t *testing.T) {
	for _, seconds := range []int{0, -1} {
		cfg := (Config{SyncTimeoutSeconds: seconds, SyncStopTimeoutSeconds: seconds}).Normalize()
		require.Equal(t, 300, cfg.SyncTimeoutSeconds)
		require.Equal(t, 5, cfg.SyncStopTimeoutSeconds)
	}
	s := NewSyncer(syncFunc(func(context.Context) (SyncStats, error) { return SyncStats{}, nil }),
		Config{SyncTimeoutSeconds: 9999, SyncStopTimeoutSeconds: 9999, SyncIntervalSeconds: int(^uint(0) >> 1)})
	require.Equal(t, time.Hour, s.runTimeout)
	require.Equal(t, 30*time.Second, s.stopTimeout)
	require.Positive(t, s.interval, "duration conversion must not overflow into run-once mode")
	cfg := (Config{SyncIntervalSeconds: -1, SyncTimeoutSeconds: 17, SyncStopTimeoutSeconds: 2}).Normalize()
	require.Equal(t, -1, cfg.SyncIntervalSeconds)
	require.Equal(t, 17, cfg.SyncTimeoutSeconds)
	require.Equal(t, 2, cfg.SyncStopTimeoutSeconds)
}
