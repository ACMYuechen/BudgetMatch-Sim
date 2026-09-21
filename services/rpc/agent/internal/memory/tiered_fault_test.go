package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// Model the durable SaveTurn boundary, not PostgreSQL transaction semantics.
type faultDurable struct {
	*fakeDurableMemory
	saveErr error
}

func (m *faultDurable) SaveTurn(ctx context.Context, req SaveTurnReq) (Conversation, Turn, error) {
	if m.saveErr != nil {
		return Conversation{}, Turn{}, m.saveErr
	}
	conversation, turn, err := m.fakeDurableMemory.SaveTurn(ctx, req)
	if err == nil {
		m.snapshot.Messages = append(m.snapshot.Messages, schema.UserMessage(req.Query), schema.AssistantMessage(req.Summary, nil))
	}
	return conversation, turn, err
}

func TestTieredFaultMatrixSaveAndInvalidation(t *testing.T) {
	for _, scenario := range []string{"committed with cache failure", "durable failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			durable := &faultDurable{fakeDurableMemory: &fakeDurableMemory{
				exists: true, snapshot: Snapshot{Version: 1, Messages: []*schema.Message{schema.UserMessage("old")}},
			}}
			cache := &fakeSnapshotCache{}
			m := newTiered(durable, cache, Conf{})
			_, err := m.History(ctx, "u", "c", 0)
			require.NoError(t, err)
			require.EqualValues(t, 1, cache.snapshot.Version)
			fault := errors.New("synthetic write boundary failure")
			if scenario == "durable failure" {
				durable.saveErr = fault
			} else {
				cache.deleteErr = fault
			}
			_, _, err = m.SaveTurn(ctx, faultTurn())
			if scenario == "durable failure" {
				require.ErrorIs(t, err, fault)
				require.Zero(t, cache.deleteCalls, "do not invalidate an uncommitted round")
				require.EqualValues(t, 1, durable.snapshot.Version)
			} else {
				require.NoError(t, err, "cache failure must not turn a committed round into failure")
				require.Equal(t, 1, cache.deleteCalls)
				require.EqualValues(t, 1, cache.snapshot.Version, "fault did not preserve stale cache")
				messages, err := m.History(ctx, "u", "c", 0)
				require.NoError(t, err)
				require.Len(t, messages, 3)
				require.Equal(t, "original", messages[2].Content)
				require.EqualValues(t, 2, cache.snapshot.Version, "must repair from the authoritative version")
			}
		})
	}
}

func TestTieredFaultMatrixCannotTrustCacheWithoutDurableVersion(t *testing.T) {
	durable := &fakeDurableMemory{exists: true, snapshot: Snapshot{Version: 1},
		versionErr: errors.New("synthetic version unavailable")}
	cache := &fakeSnapshotCache{set: true, snapshot: Snapshot{
		Version: 1, CachedLimit: 20, Messages: []*schema.Message{schema.UserMessage("unverified cache")},
	}}
	m := newTiered(durable, cache, Conf{})
	messages, err := m.History(t.Context(), "u", "c", 0)
	require.ErrorIs(t, err, durable.versionErr)
	require.Empty(t, messages)
	require.Zero(t, cache.loadCalls, "unavailable durable store is not permission to trust a cached version")
}
