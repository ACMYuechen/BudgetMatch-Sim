package product_vectors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexInitializationBindsOnlyEmptyIndexAndIsIdempotent(t *testing.T) {
	state := &sessionDatabase{}
	m := sessionModel(t, state, testProfile(), false)
	ctx := context.Background()
	require.NoError(t, m.Initialize(ctx))
	require.Equal(t, 3, state.schema.dim)
	require.Equal(t, testProfile(), *state.schema.profile)
	require.NoError(t, m.Initialize(ctx))
	bindings := 0
	for _, event := range state.events {
		if strings.HasPrefix(event.sql, "CREATE ") || strings.HasPrefix(event.sql, "INSERT ") {
			require.True(t, event.tx, "initialization DDL/binding must be transactional")
		}
		if strings.HasPrefix(event.sql, "INSERT INTO product_vector_profile") {
			bindings++
		}
		require.NotContains(t, event.sql, "DROP ")
		require.NotContains(t, event.sql, "TRUNCATE ")
	}
	require.Equal(t, 1, bindings)
	require.Nil(t, state.owner)
}

func TestIndexInitializationRefusesDestructiveOrUnknownModelChanges(t *testing.T) {
	for _, name := range []string{"different dimension", "different model", "legacy populated", "invalid binding", "index creation failure"} {
		t.Run(name, func(t *testing.T) {
			state := readySessionState()
			want := testProfile()
			expectedErr := ErrProfileMismatch
			switch name {
			case "different dimension":
				want.Dimensions = 4
			case "different model":
				want.Fingerprint = strings.Repeat("b", 64)
			case "legacy populated":
				state.schema.profile = nil
				expectedErr = ErrUnboundIndex
			case "invalid binding":
				state.schema.profile = &Profile{}
			case "index creation failure":
				state.schema.profile = nil
				state.schema.populated = false
				expectedErr = errors.New("index failed")
				state.failSQL, state.execErr = "CREATE INDEX", expectedErr
			}
			before := state.schema
			m := sessionModel(t, state, want, false)
			require.ErrorIs(t, m.Initialize(context.Background()), expectedErr)
			require.Equal(t, before, state.schema)
			require.Nil(t, state.owner)
			rolledBack := false
			for _, event := range state.events {
				rolledBack = rolledBack || event.sql == "rollback"
				for _, destructive := range []string{"DROP ", "TRUNCATE ", "DELETE ", "ALTER TABLE ", "UPDATE product_vector_profile"} {
					require.NotContains(t, event.sql, destructive)
				}
			}
			require.True(t, rolledBack)
		})
	}
}

func TestIndexInitializationAndSyncUseSameLock(t *testing.T) {
	state := readySessionState()
	a, b := sessionModel(t, state, testProfile(), false), sessionModel(t, state, testProfile(), false)
	ctx := context.Background()
	require.NoError(t, a.WithSync(ctx, a.profile.Fingerprint, func(SyncStore) error {
		require.ErrorIs(t, b.Initialize(ctx), ErrSyncBusy)
		return nil
	}))
	for _, event := range state.events {
		require.NotContains(t, event.sql, "CREATE ")
	}
}

func TestIndexProfileValidationBeforeConnecting(t *testing.T) {
	for _, profile := range []Profile{{}, {Fingerprint: "secret-not-a-digest", Dimensions: 3},
		{Fingerprint: strings.Repeat("g", 64), Dimensions: 3}, {Fingerprint: strings.Repeat("a", 64), Dimensions: 0}} {
		state := &sessionDatabase{}
		m := sessionModel(t, state, profile, false)
		require.Error(t, m.Initialize(context.Background()))
		require.Empty(t, state.connections)
	}
}

func TestVectorSearchFiltersProfileInSameStatement(t *testing.T) {
	state := readySessionState()
	m := sessionModel(t, state, testProfile(), true)
	_, err := m.SearchByVector(context.Background(), []float32{1, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, state.events, 1)
	event := state.events[0]
	require.Contains(t, event.sql, "WHERE EXISTS (SELECT 1 FROM product_vector_profile")
	require.Contains(t, event.sql, "fingerprint = $2 AND dimensions = $3")
	require.Equal(t, m.profile.Fingerprint, event.args[1].Value)
	require.Equal(t, int64(3), event.args[2].Value)
	require.NotContains(t, event.sql, m.profile.Fingerprint)
}
