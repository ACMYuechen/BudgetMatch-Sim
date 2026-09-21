package devrecords

import (
	"context"
	"errors"
	"testing"
	"time"

	"budgetmatch-sim/services/rpc/agent/internal/memory"

	"github.com/stretchr/testify/require"
)

type guardedMemory struct {
	*memory.InMemory
	saves, clears, deletes int
}

func (s *guardedMemory) SaveTurn(ctx context.Context, r memory.SaveTurnReq) (memory.Conversation, memory.Turn, error) {
	s.saves++
	return s.InMemory.SaveTurn(ctx, r)
}
func (s *guardedMemory) Clear(context.Context, string, string) error {
	s.clears++
	return errors.New("clear is forbidden")
}
func (s *guardedMemory) DeleteConversation(context.Context, string, string) (bool, error) {
	s.deletes++
	return false, errors.New("delete is forbidden")
}

func TestDemoPersistsMarkedTwoTurnHistoryAndReplaysWithoutWrites(t *testing.T) {
	o := validOptions()
	store := &guardedMemory{InMemory: memory.NewInMemory(memory.Conf{})}
	first, err := exercise(t.Context(), store, o)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.True(t, first.ReplayVerified)
	require.Equal(t, 2, first.ProviderCalls)
	require.Equal(t, 2, store.saves)
	saved, exists, err := readSnapshot(t.Context(), store, o)
	require.NoError(t, err)
	require.True(t, exists)
	require.EqualValues(t, 2, saved.Conversation.Version)
	require.EqualValues(t, 40000, saved.Conversation.State.BudgetCents)
	require.EqualValues(t, 2, saved.Conversation.State.MaxItems)
	require.Contains(t, saved.Conversation.Title, "开发演示")
	require.Len(t, saved.Turns, 2)
	require.Len(t, saved.Messages, 4)
	for _, turn := range saved.Turns {
		require.Contains(t, turn.Summary, "演示数据，非实时商城库存")
	}
	second, err := exercise(t.Context(), store, o)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Zero(t, second.ProviderCalls)
	require.Equal(t, 2, store.saves)
	after, _, err := readSnapshot(t.Context(), store, o)
	require.NoError(t, err)
	require.True(t, sameSnapshot(saved, after, false))
	require.Zero(t, store.clears)
	require.Zero(t, store.deletes)
}

func TestDemoPreservesOtherUsersAndRunNamespaces(t *testing.T) {
	store := &guardedMemory{InMemory: memory.NewInMemory(memory.Conf{})}
	o := validOptions()
	_, err := exercise(t.Context(), store, o)
	require.NoError(t, err)
	saved, _, err := readSnapshot(t.Context(), store, o)
	require.NoError(t, err)
	for _, mutate := range []func(*Options){func(o *Options) { o.UserID = "other-user" }, func(o *Options) { o.RunID = "other-run" }} {
		other := o
		mutate(&other)
		_, err := exercise(t.Context(), store, other)
		require.NoError(t, err)
	}
	after, _, err := readSnapshot(t.Context(), store, o)
	require.NoError(t, err)
	require.True(t, sameSnapshot(saved, after, false))
	require.Equal(t, 6, store.saves)
}

type changedStore struct {
	*guardedMemory
	change func(*memory.Conversation, []memory.Turn)
}

func (s changedStore) ListTurns(ctx context.Context, user, convo string, page, size int) (memory.Conversation, []memory.Turn, int64, bool, error) {
	c, turns, n, ok, err := s.InMemory.ListTurns(ctx, user, convo, page, size)
	if ok && err == nil {
		s.change(&c, turns)
	}
	return c, turns, n, ok, err
}

func TestDemoRefusesDifferentOrEditedRecordsWithoutRepair(t *testing.T) {
	for name, change := range map[string]func(*memory.Conversation, []memory.Turn){
		"title":   func(c *memory.Conversation, _ []memory.Turn) { c.Title = "existing business title" },
		"state":   func(c *memory.Conversation, _ []memory.Turn) { c.State.BudgetCents++ },
		"version": func(c *memory.Conversation, _ []memory.Turn) { c.Version++ },
		"count":   func(c *memory.Conversation, _ []memory.Turn) { c.TurnCount++ },
		"query":   func(_ *memory.Conversation, turns []memory.Turn) { turns[0].Query = "other" },
		"result": func(_ *memory.Conversation, turns []memory.Turn) {
			turns[0].ResultJSON = []byte(`{"summary":"foreign"}`)
		},
		"status":   func(_ *memory.Conversation, turns []memory.Turn) { turns[0].Status = "pending" },
		"sequence": func(_ *memory.Conversation, turns []memory.Turn) { turns[0].Sequence++ },
	} {
		t.Run(name, func(t *testing.T) {
			base := &guardedMemory{InMemory: memory.NewInMemory(memory.Conf{})}
			o := validOptions()
			_, err := exercise(t.Context(), base, o)
			require.NoError(t, err)
			_, err = exercise(t.Context(), changedStore{base, change}, o)
			require.ErrorContains(t, err, "different or partial")
			require.Equal(t, 2, base.saves)
			require.Zero(t, base.clears)
			require.Zero(t, base.deletes)
		})
	}
}

func TestDemoRejectsPartialFixtureAndReadOnlyOrCanceledExecution(t *testing.T) {
	o := validOptions()
	store := &guardedMemory{InMemory: memory.NewInMemory(memory.Conf{})}
	svc, _ := demoService(store)
	_, err := svc.Recommend(t.Context(), demoInputs(o)[0])
	require.NoError(t, err)
	_, err = exercise(t.Context(), store, o)
	require.ErrorContains(t, err, "different or partial")
	require.Equal(t, 1, store.saves)
	o.WriteDemo, o.RunID = false, ""
	_, err = exercise(t.Context(), store, o)
	require.ErrorContains(t, err, "not selected")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = exercise(ctx, store, validOptions())
	require.Error(t, err)
	require.Equal(t, 1, store.saves)
}

func TestSnapshotComparisonKeepsTimestampsOnReplayAndExactJSONNumbers(t *testing.T) {
	s, err := expectedSnapshot(t.Context(), validOptions())
	require.NoError(t, err)
	other := s
	other.Conversation.UpdatedAt = other.Conversation.UpdatedAt.Add(time.Nanosecond)
	require.True(t, sameSnapshot(s, other, true))
	require.False(t, sameSnapshot(s, other, false))
	a := demoSnapshot{Turns: []memory.Turn{{ResultJSON: []byte(`{"n":9007199254740993,"items":[]}`)}}}
	for _, raw := range []string{`{"n":9007199254740992,"items":[]}`, `{"n":9007199254740993}`, `{"n":9007199254740993,"items":null}`} {
		b := demoSnapshot{Turns: []memory.Turn{{ResultJSON: []byte(raw)}}}
		require.False(t, sameSnapshot(a, b, true))
	}
}
