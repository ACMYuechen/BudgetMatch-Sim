package storageacceptance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
	"budgetmatch-sim/services/rpc/agent/model/product_vectors"

	"github.com/stretchr/testify/require"
)

const recoveryEnv = "AGENT_STORAGE_ACCEPTANCE_RECOVERY"

type recoveryRecord struct {
	Input        agentcore.Input
	Result       *agentcore.Result
	Conversation memory.Conversation
	Turns        []memory.Turn
	Messages     []string
}

type recoveryCheckpoint struct {
	RunID   string
	Records map[string]recoveryRecord
	Hashes  map[string]string
}

// Checkpoint pretty-printing changes RawMessage whitespace, and pgx timestamps
// may use time.Local even for UTC. Compare every persisted value, not formatting
// or a Location pointer. UseNumber preserves integers above float64 precision.
func canonicalRecoveryRecord(record recoveryRecord) ([]byte, error) {
	record.Conversation.CreatedAt = record.Conversation.CreatedAt.UTC()
	record.Conversation.UpdatedAt = record.Conversation.UpdatedAt.UTC()
	record.Turns = append([]memory.Turn(nil), record.Turns...)
	for i := range record.Turns {
		record.Turns[i].CreatedAt = record.Turns[i].CreatedAt.UTC()
		record.Turns[i].CompletedAt = record.Turns[i].CompletedAt.UTC()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func TestRecoveryComparisonPreservesValuesAcrossCheckpointEncoding(t *testing.T) {
	original := recoveryRecord{Conversation: memory.Conversation{Version: 4,
		CreatedAt: time.Unix(100, 123456000).In(time.FixedZone("synthetic", 8*60*60))},
		Turns: []memory.Turn{{ResultJSON: []byte(`{"n":9007199254740993,"items":[]}`)}}}
	encoded, err := json.MarshalIndent(original, "", "  ")
	require.NoError(t, err)
	var checkpoint recoveryRecord
	require.NoError(t, json.Unmarshal(encoded, &checkpoint))
	checkpoint.Conversation.CreatedAt = checkpoint.Conversation.CreatedAt.UTC()
	want, err := canonicalRecoveryRecord(original)
	require.NoError(t, err)
	got, err := canonicalRecoveryRecord(checkpoint)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
	for _, raw := range []string{`{"n":9007199254740992,"items":[]}`, `{"n":9007199254740993}`, `{"n":9007199254740993,"items":null}`} {
		changed := original
		changed.Turns = []memory.Turn{{ResultJSON: []byte(raw)}}
		got, err := canonicalRecoveryRecord(changed)
		require.NoError(t, err)
		require.NotEqual(t, string(want), string(got), "changed/lost data was hidden by normalization")
	}
	checkpoint.Conversation.CreatedAt = checkpoint.Conversation.CreatedAt.Add(time.Nanosecond)
	got, err = canonicalRecoveryRecord(checkpoint)
	require.NoError(t, err)
	require.NotEqual(t, string(want), string(got))
	checkpoint = original
	checkpoint.Conversation.Version++
	got, err = canonicalRecoveryRecord(checkpoint)
	require.NoError(t, err)
	require.NotEqual(t, string(want), string(got))
}

func recoveryTarget(t *testing.T, phase string) (targetConfig, string) {
	t.Helper()
	actual := os.Getenv(recoveryEnv)
	if actual == "" || actual != phase && (actual == "seed" || actual == "verify") {
		t.Skip("not_run: recovery requires an explicitly selected seed/verify phase and owned external restart")
	}
	require.Equal(t, phase, actual, "unknown recovery phase")
	path := os.Getenv(configEnv)
	cfg, err := loadGrantedTarget(path, os.Getenv(grantEnv), os.Environ())
	require.NoError(t, err)
	// Check BOTH markers before seed can write, not just the first backend.
	store, err := openStore(t.Context(), cfg, "tiered")
	require.NoError(t, err)
	store.close()
	return cfg, filepath.Join(filepath.Dir(path), "recovery-checkpoint.json")
}

func readRecoveryRecord(t *testing.T, store memory.ConversationStore, in agentcore.Input) recoveryRecord {
	t.Helper()
	c, turns, count, exists, err := store.ListTurns(t.Context(), in.UserId, in.ConversationId, 1, 100)
	require.NoError(t, err)
	require.True(t, exists)
	require.EqualValues(t, 1, count)
	require.EqualValues(t, 1, c.TurnCount)
	require.Len(t, turns, 1)
	require.EqualValues(t, 1, turns[0].Sequence)
	require.Equal(t, memory.TurnStatusCompleted, turns[0].Status)
	history, err := store.History(t.Context(), in.UserId, in.ConversationId, 0)
	require.NoError(t, err)
	record := recoveryRecord{Input: in, Conversation: c, Turns: turns}
	for _, msg := range history {
		record.Messages = append(record.Messages, string(msg.Role)+":"+msg.Content)
	}
	require.Len(t, record.Messages, 2)
	return record
}

func recoveryVectorHashes(t *testing.T, cfg targetConfig) map[string]string {
	t.Helper()
	db, pool, err := cfg.openPostgres(t.Context())
	require.NoError(t, err)
	defer pool.Close()
	profile := product_vectors.Profile{Fingerprint: cfg.RunID + cfg.RunID, Dimensions: 3}
	model := product_vectors.NewProductVectorsModel(db, profile)
	// No Initialize/Upsert here: verification may not recreate missing data.
	var hashes map[string]string
	require.NoError(t, model.WithSync(t.Context(), profile.Fingerprint, func(sync product_vectors.SyncStore) error {
		var readErr error
		hashes, readErr = sync.ListHashes(t.Context())
		return readErr
	}))
	results, err := model.SearchByVector(t.Context(), []float32{1, 0, 0}, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "keep", results[0].SkuId)
	require.Equal(t, "new", results[0].ContentHash)
	require.Greater(t, results[0].Score, 0.99)
	return hashes
}

// The runner seeds once, restarts its own stores, then verifies using a NEW Go
// process. A normal go test cannot silently simulate a restart inside this test.
func TestRealStorageRecoverySeed(t *testing.T) {
	cfg, path := recoveryTarget(t, "seed")
	checkpoint := recoveryCheckpoint{RunID: cfg.RunID, Records: make(map[string]recoveryRecord)}
	for _, mode := range []string{"postgres", "redis", "tiered"} {
		t.Run(mode, func(t *testing.T) {
			store, err := openStore(t.Context(), cfg, mode)
			require.NoError(t, err)
			defer store.close()
			f := fixture{cfg: cfg, mode: mode, opened: store}
			in := f.input()
			in.ConversationId = "recovery-" + mode
			_, exists, err := store.store.GetConversation(t.Context(), in.UserId, in.ConversationId)
			require.NoError(t, err)
			require.False(t, exists, "seed requires a fresh namespace; never overwrite a prior checkpoint")
			first := f.start(t, in, "recovery-"+mode, "").done(t)
			require.Empty(t, first.Error)
			require.Equal(t, 1, first.Calls)
			record := readRecoveryRecord(t, store.store, in)
			record.Result = first.Result
			checkpoint.Records[mode] = record
		})
	}
	if t.Failed() {
		return
	}
	checkpoint.Hashes = recoveryVectorHashes(t, cfg)
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	require.NoError(t, err)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	require.NoError(t, err, "checkpoint must be new, not a symlink or overwritten artifact")
	defer file.Close()
	_, err = file.Write(data)
	require.NoError(t, err)
	require.NoError(t, file.Sync())
}

func TestRealStorageRecoveryReplay(t *testing.T) {
	cfg, path := recoveryTarget(t, "verify")
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	require.Zero(t, info.Mode().Perm()&0077)
	require.Less(t, info.Size(), int64(64*1024))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var checkpoint recoveryCheckpoint
	require.NoError(t, json.Unmarshal(data, &checkpoint))
	require.Equal(t, cfg.RunID, checkpoint.RunID)
	require.Len(t, checkpoint.Records, 3)
	for _, mode := range []string{"postgres", "redis", "tiered"} {
		t.Run(mode, func(t *testing.T) {
			expected, exists := checkpoint.Records[mode]
			require.True(t, exists)
			store, err := openStore(t.Context(), cfg, mode)
			require.NoError(t, err)
			defer store.close()
			f := fixture{cfg: cfg, mode: mode, opened: store}
			replay := f.start(t, expected.Input, "must-not-generate", "").done(t)
			require.Empty(t, replay.Error)
			require.Zero(t, replay.Calls, "recovery lost the committed turn and generated again")
			record := readRecoveryRecord(t, store.store, expected.Input)
			record.Result = replay.Result
			want, err := canonicalRecoveryRecord(expected)
			require.NoError(t, err)
			got, err := canonicalRecoveryRecord(record)
			require.NoError(t, err)
			require.Equal(t, string(want), string(got), "recovery changed the committed result/state/history/version")
		})
	}
	require.Equal(t, checkpoint.Hashes, recoveryVectorHashes(t, cfg))
}
