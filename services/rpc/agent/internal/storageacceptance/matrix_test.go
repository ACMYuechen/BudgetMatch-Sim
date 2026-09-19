package storageacceptance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	cfg    targetConfig
	mode   string
	opened *openedStore
	expire func(*testing.T, agentcore.Input)
}

func testTarget() targetConfig {
	return targetConfig{RunID: strings.ReplaceAll(uuid.NewString(), "-", ""),
		PostgresAddress: "127.0.0.1:25432", PostgresPassword: uuid.NewString(),
		RedisAddress: "127.0.0.1:26379", RedisPassword: uuid.NewString()}
}

// This ALWAYS runs, but uses a protocol simulator, not a Redis server. It proves
// the process/gate/kill harness itself works and must not close M6.2 acceptance.
func TestSimulatedRedisProcessMatrix(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := testTarget()
	cfg.RedisAddress = mr.Addr()
	mr.RequireAuth(cfg.RedisPassword)
	require.NoError(t, mr.Set(ownerKey, cfg.RunID))
	s, err := openStore(t.Context(), cfg, "redis")
	require.NoError(t, err)
	t.Cleanup(s.close)
	f := fixture{cfg: cfg, mode: "redis", opened: s, expire: func(t *testing.T, _ agentcore.Input) {
		t.Helper()
		mr.FastForward(2 * time.Minute)
	}}
	runProcessMatrix(t, f)
}

// This is the ONLY real-storage entry point. A missing grant is a visible skip;
// a partial/invalid grant or unavailable/wrong target is a failure, never a skip.
func TestRealStorageAcceptance(t *testing.T) {
	path, grant := os.Getenv(configEnv), os.Getenv(grantEnv)
	if path == "" && grant == "" {
		t.Skip("not_run: real storage requires a private acceptance config and matching explicit run_id grant")
	}
	cfg, err := loadGrantedTarget(path, grant, os.Environ())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Verify both markers before any migration or fault injection.
	s, err := openStore(ctx, cfg, "tiered")
	require.NoError(t, err)
	t.Cleanup(s.close)
	var unexpected int64
	require.NoError(t, s.pg.WithContext(ctx).Raw(`SELECT count(*) FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r','p','v','m','f')
		AND c.relname NOT IN ('agent_storage_acceptance_guard', 'agent_conversations',
		'agent_conversation_turns', 'product_vectors', 'product_vector_profile')`).Scan(&unexpected).Error)
	require.Zero(t, unexpected, "refusing a database containing unrelated relations")
	var vectorInstalled bool
	require.NoError(t, s.pg.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&vectorInstalled).Error)
	require.True(t, vectorInstalled, "provision pgvector in the disposable database before granting acceptance")
	var postgresVersion, vectorVersion string
	require.NoError(t, s.pg.WithContext(ctx).Raw("SHOW server_version").Scan(&postgresVersion).Error)
	require.NoError(t, s.pg.WithContext(ctx).Raw("SELECT extversion FROM pg_extension WHERE extname = 'vector'").Scan(&vectorVersion).Error)
	info, err := s.rdb.Info(ctx, "server").Result()
	require.NoError(t, err)
	var redisVersion string
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			redisVersion = strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
		}
	}
	require.NotEmpty(t, redisVersion)
	t.Logf("run_id=%s postgres=%s pgvector=%s redis=%s", cfg.RunID, postgresVersion, vectorVersion, redisVersion)
	migration := memory.NewPostgres(s.pg.WithContext(ctx), memory.Conf{})
	require.NoError(t, migration.CreateTable())
	require.NoError(t, migration.CheckSchema())
	for _, mode := range []string{"postgres", "redis"} {
		t.Run(mode, func(t *testing.T) {
			opened, err := openStore(t.Context(), cfg, mode)
			require.NoError(t, err)
			t.Cleanup(opened.close)
			f := fixture{cfg: cfg, mode: mode, opened: opened, expire: func(t *testing.T, in agentcore.Input) {
				t.Helper()
				// Accelerate ONLY this run's owned lease. This is an explicit fault
				// injection, not evidence of waiting through the full 2-minute TTL.
				key := "agent:user:" + in.UserId + ":conv:" + in.ConversationId + ":lock"
				require.True(t, strings.HasPrefix(in.UserId, cfg.RunID+"-"))
				ok, err := opened.rdb.PExpire(t.Context(), key, 10*time.Millisecond).Result()
				require.NoError(t, err)
				require.True(t, ok, "expected the stopped worker's lease to remain")
				require.Eventually(t, func() bool {
					n, err := opened.rdb.Exists(t.Context(), key).Result()
					return err == nil && n == 0
				}, 2*time.Second, 10*time.Millisecond)
			}}
			runProcessMatrix(t, f)
		})
	}
	t.Run("postgres-transaction-and-pool", func(t *testing.T) { testPostgresBoundaries(t, cfg, s) })
	t.Run("tiered-cache-failure", func(t *testing.T) { testTieredCacheFailure(t, cfg, s) })
	t.Run("vector-publication-recovery", func(t *testing.T) { testVectorRecovery(t, cfg, s) })
}

func (f fixture) input() agentcore.Input {
	return agentcore.Input{UserId: f.cfg.RunID + "-user", ConversationId: uuid.NewString(),
		TurnId: "turn-1", Query: "键盘", BudgetCents: 30000, MaxItems: 2}
}

func (f fixture) start(t *testing.T, in agentcore.Input, label, pause string) *childProcess {
	t.Helper()
	return startChild(t, workerRequest{Target: f.cfg, Mode: f.mode, Input: in, Label: label, Pause: pause})
}

func (f fixture) assertCount(t *testing.T, in agentcore.Input, count int64) []memory.Turn {
	t.Helper()
	c, turns, total, exists, err := f.opened.store.ListTurns(t.Context(), in.UserId, in.ConversationId, 1, 100)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, count, total)
	require.Equal(t, count, c.TurnCount)
	require.Len(t, turns, int(count))
	for i, turn := range turns {
		require.EqualValues(t, i+1, turn.Sequence)
		require.Equal(t, memory.TurnStatusCompleted, turn.Status)
	}
	return turns
}

func runProcessMatrix(t *testing.T, f fixture) {
	t.Helper()
	t.Run("same-turn-replay", func(t *testing.T) {
		in := f.input()
		a := f.start(t, in, "first", "before-save")
		a.until(t, "before-save")
		b := f.start(t, in, "retry", "")
		b.until(t, "acquiring")
		a.send(t, "continue")
		first, replay := a.done(t), b.done(t)
		require.Empty(t, first.Error)
		require.Empty(t, replay.Error)
		require.Equal(t, 1, first.Calls)
		require.Zero(t, replay.Calls)
		require.Equal(t, first.Result, replay.Result)
		f.assertCount(t, in, 1)
	})
	t.Run("ordered-turn-inherits-committed-state", func(t *testing.T) {
		in := f.input()
		a := f.start(t, in, "first", "before-save")
		a.until(t, "before-save")
		next := in
		next.TurnId, next.Query, next.BudgetCents, next.MaxItems = "turn-2", "继续", 0, 0
		b := f.start(t, next, "next", "")
		b.until(t, "acquiring")
		a.send(t, "continue")
		require.Empty(t, a.done(t).Error)
		result := b.done(t)
		require.Empty(t, result.Error)
		require.NotNil(t, result.Result)
		require.Equal(t, in.BudgetCents, result.Result.Intent.BudgetCents)
		require.Equal(t, in.MaxItems, result.Result.Intent.MaxItems)
		var prior *agentcore.Intent
		for _, e := range b.history {
			if e.Stage == "model" {
				prior = e.Prior
			}
		}
		require.NotNil(t, prior, "next process did not see the previous committed intent")
		require.Equal(t, in.BudgetCents, prior.BudgetCents)
		f.assertCount(t, in, 2)
	})
	t.Run("user-and-conversation-isolation", func(t *testing.T) {
		in := f.input()
		a := f.start(t, in, "held", "before-save")
		a.until(t, "before-save")
		otherConversation, otherUser := in, in
		otherConversation.ConversationId = uuid.NewString()
		otherUser.UserId += "-other"
		for _, other := range []agentcore.Input{otherConversation, otherUser} {
			result := f.start(t, other, "independent", "").done(t)
			require.Empty(t, result.Error)
			require.Equal(t, 1, result.Calls, "different user must not replay another user's turn")
			f.assertCount(t, other, 1)
		}
		_, found, err := f.opened.store.FindTurn(t.Context(), in.UserId, in.ConversationId, in.TurnId)
		require.NoError(t, err)
		require.False(t, found, "held process committed without being released")
		a.send(t, "continue")
		require.Empty(t, a.done(t).Error)
		f.assertCount(t, in, 1)
	})
	t.Run("lock-wait-deadline", func(t *testing.T) {
		in := f.input()
		a := f.start(t, in, "held", "before-save")
		a.until(t, "before-save")
		b := startChild(t, workerRequest{Target: f.cfg, Mode: f.mode, Input: in, Label: "canceled", TimeoutMS: 250})
		canceled := b.done(t)
		require.Equal(t, "canceled", canceled.Error)
		require.Zero(t, canceled.Calls)
		require.Nil(t, canceled.Result)
		a.send(t, "continue")
		require.Empty(t, a.done(t).Error)
		retry := f.start(t, in, "retry", "").done(t)
		require.Empty(t, retry.Error)
		require.Zero(t, retry.Calls)
		f.assertCount(t, in, 1)
	})
	for _, stage := range []string{"before-save", "after-save"} {
		t.Run("process-kill-"+stage, func(t *testing.T) {
			in := f.input()
			a := f.start(t, in, "killed", stage)
			a.until(t, stage)
			a.kill(t) // abrupt OS termination: no Service defer or unlock runs
			_, found, err := f.opened.store.FindTurn(t.Context(), in.UserId, in.ConversationId, in.TurnId)
			require.NoError(t, err)
			require.Equal(t, stage == "after-save", found)
			if f.mode == "redis" {
				f.expire(t, in)
			}
			retry := f.start(t, in, "retry", "").done(t)
			require.Empty(t, retry.Error)
			require.NotNil(t, retry.Result)
			if stage == "after-save" {
				require.Zero(t, retry.Calls)
				require.Equal(t, "synthetic-killed", retry.Result.Summary)
			} else {
				require.Equal(t, 1, retry.Calls, "generation before commit is NOT exactly once")
			}
			f.assertCount(t, in, 1)
		})
	}
	t.Run("delete-orders-with-recommendation-and-allows-recreation", func(t *testing.T) {
		in := f.input()
		a := f.start(t, in, "held", "before-save")
		a.until(t, "before-save")
		b := startChild(t, workerRequest{Target: f.cfg, Mode: f.mode, Input: in, Delete: true})
		b.until(t, "acquiring")
		a.send(t, "continue")
		require.Empty(t, a.done(t).Error)
		deleted := b.done(t)
		require.Empty(t, deleted.Error)
		require.True(t, deleted.Deleted)
		_, found, err := f.opened.store.FindTurn(t.Context(), in.UserId, in.ConversationId, in.TurnId)
		require.NoError(t, err)
		require.False(t, found, "delete must cascade to completed turns")
		_, exists, err := f.opened.store.GetConversation(t.Context(), in.UserId, in.ConversationId)
		require.NoError(t, err)
		require.False(t, exists)
		recreated := f.start(t, in, "recreated", "").done(t)
		require.Empty(t, recreated.Error)
		require.Equal(t, 1, recreated.Calls)
		f.assertCount(t, in, 1)
	})
	if f.mode == "redis" {
		t.Run("expired-owner-cannot-overwrite-successor", func(t *testing.T) {
			in := f.input()
			a := f.start(t, in, "stale", "before-save")
			a.until(t, "before-save")
			f.expire(t, in)
			winner := f.start(t, in, "successor", "").done(t)
			require.Empty(t, winner.Error)
			a.send(t, "continue")
			require.Equal(t, "lease-lost", a.done(t).Error)
			turns := f.assertCount(t, in, 1)
			var stored agentcore.Result
			require.NoError(t, json.Unmarshal(turns[0].ResultJSON, &stored))
			require.Equal(t, "synthetic-successor", stored.Summary)
		})
	}
}
