package memory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	redisConversationLeaseTTL = 2 * time.Minute
	redisConversationBudget   = 90 * time.Second
	redisReleaseTimeout       = 5 * time.Second
)

// ErrConversationLeaseLost rejects a stale or incorrectly scoped Redis holder.
// A caller must retry through the normal turn-id replay path, not retry a write
// using old generated state under a newly acquired lease.
var ErrConversationLeaseLost = errors.New("memory: redis conversation lease lost")

type redisLeaseKey struct{}
type redisLease struct {
	owner      *Redis
	key, token string
}

// WithConversationLock bounds waiting + execution below the lease TTL. The
// deadline is cooperative; WATCH at the write boundary also rejects a holder
// whose lease expired or was replaced despite that deadline (e.g. a pause).
func (m *Redis) WithConversationLock(ctx context.Context, userId, conversationId string, fn func(context.Context) error) (retErr error) {
	if m == nil || m.client == nil {
		return fmt.Errorf("memory: redis client is nil")
	}
	if userId == "" || conversationId == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := conversationLockRedisKey(userId, conversationId)
	if lease, ok := ctx.Value(redisLeaseKey{}).(*redisLease); ok {
		if lease.owner != m || lease.key != key {
			return ErrConversationLeaseLost
		}
		return fn(ctx) // Reuse this request's lease; never silently reacquire it.
	}
	ctx, cancel := context.WithTimeout(ctx, redisConversationBudget)
	defer cancel()
	token := uuid.NewString()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		created, err := m.client.SetNX(ctx, key, token, redisConversationLeaseTTL).Result()
		if err != nil {
			return fmt.Errorf("memory: acquire redis conversation lock: %w", err)
		}
		if created {
			defer func() {
				// Cleanup may outlive cancellation, but not with an unbounded context.
				releaseCtx, releaseCancel := context.WithTimeout(context.Background(), redisReleaseTimeout)
				defer releaseCancel()
				const releaseScript = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
				removed, releaseErr := m.client.Eval(releaseCtx, releaseScript, []string{key}, token).Int64()
				if retErr != nil {
					return // Preserve the execution error, including cancellation.
				}
				switch {
				case ctx.Err() != nil:
					retErr = ctx.Err()
				case releaseErr != nil:
					retErr = fmt.Errorf("memory: release redis conversation lock: %w", releaseErr)
				case removed != 1:
					retErr = ErrConversationLeaseLost
				}
			}()
			return fn(context.WithValue(ctx, redisLeaseKey{}, &redisLease{owner: m, key: key, token: token}))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// writeWithLease checks the owner on the SAME connection whose EXEC watches
// the lock key. A GET followed by an unrelated transaction leaves an expiry
// race. All callback reads must use tx too, including when the pool has size 1.
// Direct legacy mutations acquire a lease; service mutations reuse theirs.
func (m *Redis) writeWithLease(ctx context.Context, userId, conversationId string, write func(context.Context, *redis.Tx) error) error {
	return m.WithConversationLock(ctx, userId, conversationId, func(locked context.Context) error {
		lease := locked.Value(redisLeaseKey{}).(*redisLease)
		err := m.client.Watch(locked, func(tx *redis.Tx) error {
			token, err := tx.Get(locked, lease.key).Result()
			if err == redis.Nil || err == nil && token != lease.token {
				return ErrConversationLeaseLost
			}
			if err != nil {
				return fmt.Errorf("memory: check redis conversation lease: %w", err)
			}
			if err := locked.Err(); err != nil {
				return err
			}
			return write(locked, tx)
		}, lease.key)
		if errors.Is(err, redis.TxFailedErr) {
			return ErrConversationLeaseLost // No automatic retry with stale state.
		}
		return err
	})
}
