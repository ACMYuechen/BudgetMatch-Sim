package memory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This is a real go-redis client against an isolated protocol simulator, NOT
// evidence of Redis server failover, persistence, Cluster or multi-process safety.
func newFaultRedis(t *testing.T) (*Redis, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), PoolSize: 1,
		MaxRetries: -1, ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedis(client, Conf{TTL: time.Hour}), mr, client
}

func faultTurn() SaveTurnReq {
	return SaveTurnReq{UserId: "u", ConversationId: "c", TurnId: "original",
		Query: "original", Summary: "original", Title: "original",
		ResultJSON: []byte(`{"summary":"original"}`)}
}

func redisFaultMutations() map[string]func(context.Context, *Redis) error {
	return map[string]func(context.Context, *Redis) error{
		"save": func(ctx context.Context, m *Redis) error {
			req := faultTurn()
			req.TurnId, req.Summary = "late", "must not commit"
			_, _, err := m.SaveTurn(ctx, req)
			return err
		},
		"delete": func(ctx context.Context, m *Redis) error {
			_, err := m.DeleteConversation(ctx, "u", "c")
			return err
		},
		"append": func(ctx context.Context, m *Redis) error {
			return m.Append(ctx, "u", "c", schema.UserMessage("late"))
		},
		"title": func(ctx context.Context, m *Redis) error {
			_, err := m.GetOrCreateTitle(ctx, "u", "c", "late")
			return err
		},
	}
}

func TestRedisLeaseExpiryRejectsEveryConversationWrite(t *testing.T) {
	for name, mutate := range redisFaultMutations() {
		t.Run(name, func(t *testing.T) {
			m, mr, _ := newFaultRedis(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, _, err := m.SaveTurn(ctx, faultTurn())
			require.NoError(t, err)
			if name == "title" {
				mr.Del(titleKey("u", "c"))
			}
			err = m.WithConversationLock(ctx, "u", "c", func(locked context.Context) error {
				// Expire only in the simulator; do not spend minutes or use sleeps.
				mr.FastForward(2 * time.Minute)
				require.NoError(t, mr.Set(conversationLockRedisKey("u", "c"), "successor"))
				before := mr.Dump()
				writeErr := mutate(locked, m)
				require.ErrorIs(t, writeErr, ErrConversationLeaseLost, "an expired holder must not write")
				require.Equal(t, before, mr.Dump(), "rejected write changed Redis data")
				return writeErr
			})
			require.Error(t, err)
			token, err := mr.Get(conversationLockRedisKey("u", "c"))
			require.NoError(t, err)
			require.Equal(t, "successor", token, "old cleanup deleted the successor lock")
		})
	}
}

type beforeExecHook struct {
	armed atomic.Bool
	run   func()
}

func (*beforeExecHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (*beforeExecHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *beforeExecHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if len(cmds) > 0 && cmds[0].Name() == "multi" && h.armed.CompareAndSwap(true, false) {
			h.run()
		}
		return next(ctx, cmds)
	}
}

func TestRedisLeaseChangeImmediatelyBeforeCommitRejectsAllWrites(t *testing.T) {
	for name, mutate := range redisFaultMutations() {
		for _, replacement := range []bool{false, true} {
			mode := "expired"
			if replacement {
				mode = "replaced"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				m, mr, client := newFaultRedis(t)
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				_, _, err := m.SaveTurn(ctx, faultTurn())
				require.NoError(t, err)
				if name == "title" {
					mr.Del(titleKey("u", "c"))
				}
				var before string
				hook := &beforeExecHook{run: func() {
					mr.FastForward(redisConversationLeaseTTL)
					if replacement {
						require.NoError(t, mr.Set(conversationLockRedisKey("u", "c"), "successor"))
					}
					before = mr.Dump()
				}}
				client.AddHook(hook)
				err = m.WithConversationLock(ctx, "u", "c", func(locked context.Context) error {
					hook.armed.Store(true)
					return mutate(locked, m)
				})
				require.NotEmpty(t, before, "commit fault was not injected")
				require.ErrorIs(t, err, ErrConversationLeaseLost, "token check and commit must be indivisible")
				require.Equal(t, before, mr.Dump())
			})
		}
	}
}

// Keep the hook's dependency on the public interface explicit.
var _ redis.Hook = (*beforeExecHook)(nil)

func TestRedisLeaseContextScopeBudgetAndCancellation(t *testing.T) {
	m, mr, client := newFaultRedis(t)
	ctx := context.WithValue(t.Context(), struct{ name string }{"request"}, "retained")
	var expired context.Context
	require.NoError(t, m.WithConversationLock(ctx, "u", "c", func(locked context.Context) error {
		expired = locked
		deadline, ok := locked.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), redisConversationBudget)
		require.Less(t, redisConversationBudget, redisConversationLeaseTTL)
		require.Equal(t, "retained", locked.Value(struct{ name string }{"request"}))
		token, err := mr.Get(conversationLockRedisKey("u", "c"))
		require.NoError(t, err)
		require.NoError(t, m.WithConversationLock(locked, "u", "c", func(nested context.Context) error {
			require.Same(t, locked.Value(redisLeaseKey{}), nested.Value(redisLeaseKey{}))
			return nil
		}))
		again, err := mr.Get(conversationLockRedisKey("u", "c"))
		require.NoError(t, err)
		require.Equal(t, token, again, "nested operation must not unlock or reacquire")
		before := mr.Dump()
		for _, req := range []SaveTurnReq{
			{UserId: "other", ConversationId: "c", TurnId: "t", ResultJSON: []byte(`{}`)},
			{UserId: "u", ConversationId: "other", TurnId: "t", ResultJSON: []byte(`{}`)},
		} {
			_, _, err := m.SaveTurn(locked, req)
			require.ErrorIs(t, err, ErrConversationLeaseLost)
		}
		_, _, err = NewRedis(client, Conf{}).SaveTurn(locked, faultTurn())
		require.ErrorIs(t, err, ErrConversationLeaseLost, "lease is bound to its store")
		require.Equal(t, before, mr.Dump())
		return nil
	}))
	require.ErrorIs(t, expired.Err(), context.Canceled)
	_, _, err := m.SaveTurn(expired, faultTurn())
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, mr.Keys(), "expired callback context must not reacquire or write")

	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	expected, _ := short.Deadline()
	require.NoError(t, m.WithConversationLock(short, "u", "c", func(locked context.Context) error {
		actual, _ := locked.Deadline()
		require.Equal(t, expected, actual, "do not lengthen caller deadline")
		return nil
	}))
	// A waiting, canceled request must not invoke its callback or remove the holder.
	require.NoError(t, mr.Set(conversationLockRedisKey("u", "c"), "held"))
	waiter, stop := context.WithCancel(ctx)
	stop()
	err = m.WithConversationLock(waiter, "u", "c", func(context.Context) error {
		t.Fatal("canceled waiter entered callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	token, err := mr.Get(conversationLockRedisKey("u", "c"))
	require.NoError(t, err)
	require.Equal(t, "held", token)
}

type releaseFaultHook struct {
	beforeExecHook
	fault   atomic.Bool
	checked atomic.Int32
	err     error
	t       *testing.T
}

func (h *releaseFaultHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() != "eval" {
			return next(ctx, cmd)
		}
		h.checked.Add(1)
		deadline, ok := ctx.Deadline()
		require.True(h.t, ok, "cleanup must have a deadline")
		require.LessOrEqual(h.t, time.Until(deadline), redisReleaseTimeout)
		require.NoError(h.t, ctx.Err(), "cleanup must survive caller cancellation")
		err := next(ctx, cmd)
		if err == nil && h.fault.CompareAndSwap(true, false) {
			return h.err // The unlock happened, but its acknowledgement was lost.
		}
		return err
	}
}

func TestRedisLeaseCleanupAndCommittedRetry(t *testing.T) {
	m, mr, client := newFaultRedis(t)
	hook := &releaseFaultHook{t: t, err: errors.New("synthetic lost release acknowledgement")}
	client.AddHook(hook)
	ctx := t.Context()
	hook.fault.Store(true)
	_, _, err := m.SaveTurn(ctx, faultTurn())
	require.ErrorIs(t, err, hook.err)
	_, found, err := m.FindTurn(ctx, "u", "c", "original")
	require.NoError(t, err)
	require.True(t, found, "release error is not proof that commit failed")
	again, turn, err := m.SaveTurn(ctx, faultTurn())
	require.NoError(t, err)
	require.EqualValues(t, 1, again.TurnCount)
	require.EqualValues(t, 1, turn.Sequence)

	canceled, cancel := context.WithCancel(ctx)
	err = m.WithConversationLock(canceled, "u", "c", func(context.Context) error {
		cancel()
		return context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, mr.Exists(conversationLockRedisKey("u", "c")))
	require.Panics(t, func() {
		_ = m.WithConversationLock(ctx, "u", "c", func(context.Context) error { panic("synthetic") })
	})
	require.False(t, mr.Exists(conversationLockRedisKey("u", "c")))
	require.EqualValues(t, 4, hook.checked.Load())
}

type cutTransactionConn struct {
	net.Conn
	cut      *atomic.Bool
	attempts *atomic.Int32
}

func (c *cutTransactionConn) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("$5\r\nmulti\r\n")) {
		c.attempts.Add(1)
		if c.cut.CompareAndSwap(true, false) {
			return 0, io.ErrUnexpectedEOF // nothing in this transaction reached Redis
		}
	}
	return c.Conn.Write(data)
}

func TestRedisLeaseConnectionFailureCannotRetryWithoutWatch(t *testing.T) {
	mr := miniredis.RunT(t)
	var cut atomic.Bool
	var attempts atomic.Int32
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), PoolSize: 1,
		ContextTimeoutEnabled: true, MaxRetries: 2, MinRetryBackoff: time.Millisecond, MaxRetryBackoff: time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &cutTransactionConn{Conn: conn, cut: &cut, attempts: &attempts}, nil
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	m := NewRedis(client, Conf{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, _, err := m.SaveTurn(ctx, faultTurn())
	require.NoError(t, err)
	attempts.Store(0)
	cut.Store(true)
	req := faultTurn()
	req.TurnId = "after-socket-cut"
	_, _, err = m.SaveTurn(ctx, req)
	require.Error(t, err)
	require.False(t, cut.Load(), "fault was not injected at the transaction write")
	require.EqualValues(t, 1, attempts.Load(), "broken WATCH connection must not reconnect and resend an unguarded transaction")
	_, found, err := m.FindTurn(ctx, req.UserId, req.ConversationId, req.TurnId)
	require.NoError(t, err)
	require.False(t, found)
	conversation, turn, err := m.SaveTurn(ctx, req)
	require.NoError(t, err, "new request must acquire and watch a fresh lease")
	require.EqualValues(t, 2, conversation.TurnCount)
	require.EqualValues(t, 2, turn.Sequence)
}
