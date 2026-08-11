package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestTieredHistoryReadThroughAndCacheHit(t *testing.T) {
	ctx := context.Background()
	durable := &fakeDurableMemory{
		exists: true,
		snapshot: Snapshot{
			Version:          7,
			Title:            "学习桌面",
			TitleInitialized: true,
			Messages: []*schema.Message{
				schema.UserMessage("第一轮"),
				schema.AssistantMessage("回答第一轮", nil),
				schema.UserMessage("第二轮"),
				schema.AssistantMessage("回答第二轮", nil),
			},
		},
	}
	cache := &fakeSnapshotCache{}
	m := newTiered(durable, cache, Conf{MaxHistory: 4})

	first, err := m.History(ctx, "u1", "c1", 2)
	if err != nil {
		t.Fatalf("History() first error = %v", err)
	}
	if len(first) != 2 || first[0].Content != "第二轮" || first[1].Content != "回答第二轮" {
		t.Fatalf("unexpected first history: %+v", first)
	}
	if durable.loadCalls != 1 || cache.storeCalls != 1 {
		t.Fatalf("expected one read-through, durable loads=%d cache stores=%d", durable.loadCalls, cache.storeCalls)
	}

	// 修改返回值不能污染 Redis 中序列化保存的快照。
	first[0].Content = "被调用方修改"
	second, err := m.History(ctx, "u1", "c1", 2)
	if err != nil {
		t.Fatalf("History() cached error = %v", err)
	}
	if second[0].Content != "第二轮" {
		t.Fatalf("cached snapshot was mutated: %+v", second)
	}
	if durable.loadCalls != 1 || cache.loadCalls != 2 {
		t.Fatalf("second read should hit cache, durable loads=%d cache loads=%d", durable.loadCalls, cache.loadCalls)
	}
}

func TestTieredStaleCacheFallsBackAfterInvalidationFailure(t *testing.T) {
	ctx := context.Background()
	durable := &fakeDurableMemory{
		exists: true,
		snapshot: Snapshot{
			Version: 1,
			Messages: []*schema.Message{
				schema.UserMessage("第一轮"),
				schema.AssistantMessage("回答第一轮", nil),
			},
		},
	}
	cache := &fakeSnapshotCache{}
	m := newTiered(durable, cache, Conf{MaxHistory: 4})

	if _, err := m.History(ctx, "u1", "c1", 4); err != nil {
		t.Fatalf("prime cache error = %v", err)
	}
	cache.deleteErr = errors.New("redis unavailable")
	if err := m.Append(ctx, "u1", "c1",
		schema.UserMessage("第二轮"),
		schema.AssistantMessage("回答第二轮", nil)); err != nil {
		t.Fatalf("Append() should succeed after PostgreSQL commit: %v", err)
	}

	got, err := m.History(ctx, "u1", "c1", 4)
	if err != nil {
		t.Fatalf("History() after stale cache error = %v", err)
	}
	if len(got) != 4 || got[2].Content != "第二轮" {
		t.Fatalf("stale cache was returned: %+v", got)
	}
	if durable.loadCalls != 2 {
		t.Fatalf("version mismatch should reload PostgreSQL, loads=%d", durable.loadCalls)
	}
}

func TestTieredCacheFailureDoesNotBreakDurableReads(t *testing.T) {
	ctx := context.Background()
	durable := &fakeDurableMemory{
		exists: true,
		snapshot: Snapshot{
			Version:  3,
			Messages: []*schema.Message{schema.UserMessage("持久化内容")},
		},
	}
	cache := &fakeSnapshotCache{
		loadErr:  errors.New("redis read failed"),
		storeErr: errors.New("redis write failed"),
		clearErr: errors.New("redis clear failed"),
	}
	m := newTiered(durable, cache, Conf{})

	got, err := m.History(ctx, "u1", "c1", 0)
	if err != nil {
		t.Fatalf("History() should fall back to PostgreSQL: %v", err)
	}
	if len(got) != 1 || got[0].Content != "持久化内容" {
		t.Fatalf("unexpected durable fallback: %+v", got)
	}
	if err := m.Clear(ctx, "u1", "c1"); err != nil {
		t.Fatalf("Clear() should use PostgreSQL result: %v", err)
	}
	if durable.clearCalls != 1 {
		t.Fatalf("durable Clear() calls = %d", durable.clearCalls)
	}
}

func TestTieredDoesNotInvalidateCacheWhenDurableAppendFails(t *testing.T) {
	durable := &fakeDurableMemory{appendErr: errors.New("postgres write failed")}
	cache := &fakeSnapshotCache{}
	m := newTiered(durable, cache, Conf{})

	err := m.Append(context.Background(), "u1", "c1", schema.UserMessage("不会落库"))
	if err == nil {
		t.Fatal("expected durable append error")
	}
	if cache.deleteCalls != 0 {
		t.Fatalf("cache should remain untouched before durable commit, deletes=%d", cache.deleteCalls)
	}
}

func TestTieredTitleUsesVersionedSnapshot(t *testing.T) {
	ctx := context.Background()
	durable := &fakeDurableMemory{
		exists: true,
		snapshot: Snapshot{
			Version:          5,
			Title:            "首次标题",
			TitleInitialized: true,
		},
	}
	cache := &fakeSnapshotCache{
		set: true,
		snapshot: Snapshot{
			Version:          5,
			CachedLimit:      20,
			Title:            "首次标题",
			TitleInitialized: true,
		},
	}
	m := newTiered(durable, cache, Conf{})

	title, err := m.GetOrCreateTitle(ctx, "u1", "c1", "不应覆盖")
	if err != nil || title != "首次标题" {
		t.Fatalf("GetOrCreateTitle() = %q, %v", title, err)
	}
	if durable.loadCalls != 0 || durable.titleCalls != 0 {
		t.Fatalf("versioned title should hit cache, loads=%d title writes=%d", durable.loadCalls, durable.titleCalls)
	}
}

func TestTieredCreatesMissingTitleInDurableStore(t *testing.T) {
	durable := &fakeDurableMemory{}
	cache := &fakeSnapshotCache{}
	m := newTiered(durable, cache, Conf{})

	title, err := m.GetOrCreateTitle(context.Background(), "u1", "c1", "新标题")
	if err != nil || title != "新标题" {
		t.Fatalf("GetOrCreateTitle() = %q, %v", title, err)
	}
	if durable.titleCalls != 1 || cache.deleteCalls != 1 {
		t.Fatalf("expected durable title creation and invalidation, title=%d deletes=%d", durable.titleCalls, cache.deleteCalls)
	}
}

type fakeDurableMemory struct {
	snapshot Snapshot
	exists   bool

	versionErr error
	loadErr    error
	appendErr  error
	titleErr   error
	clearErr   error

	versionCalls int
	loadCalls    int
	appendCalls  int
	titleCalls   int
	clearCalls   int
}

func (m *fakeDurableMemory) Version(context.Context, string, string) (int64, bool, error) {
	m.versionCalls++
	if m.versionErr != nil {
		return 0, false, m.versionErr
	}
	return m.snapshot.Version, m.exists, nil
}

func (m *fakeDurableMemory) LoadSnapshot(_ context.Context, _, _ string, limit int) (Snapshot, bool, error) {
	m.loadCalls++
	if m.loadErr != nil {
		return Snapshot{}, false, m.loadErr
	}
	if !m.exists {
		return Snapshot{}, false, nil
	}
	snapshot := cloneTestSnapshot(m.snapshot)
	snapshot.CachedLimit = limit
	snapshot.Messages = recentSnapshotMessages(snapshot, limit)
	return snapshot, true, nil
}

func (m *fakeDurableMemory) Append(_ context.Context, _, _ string, msgs ...*schema.Message) error {
	m.appendCalls++
	if m.appendErr != nil {
		return m.appendErr
	}
	if m.exists {
		m.snapshot.Version++
	} else {
		m.exists = true
		m.snapshot.Version = 1
	}
	for _, msg := range msgs {
		m.snapshot.Messages = append(m.snapshot.Messages, cloneTestMessage(msg))
	}
	return nil
}

func (m *fakeDurableMemory) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	snapshot, exists, err := m.LoadSnapshot(ctx, userId, conversationId, limit)
	if err != nil || !exists {
		return nil, err
	}
	return snapshot.Messages, nil
}

func (m *fakeDurableMemory) GetOrCreateTitle(_ context.Context, _, _ string, candidate string) (string, error) {
	m.titleCalls++
	if m.titleErr != nil {
		return "", m.titleErr
	}
	wasExisting := m.exists
	if !m.exists {
		m.exists = true
		m.snapshot.Version = 1
	}
	if !m.snapshot.TitleInitialized {
		m.snapshot.Title = candidate
		m.snapshot.TitleInitialized = true
		if wasExisting {
			m.snapshot.Version++
		}
	}
	return m.snapshot.Title, nil
}

func (m *fakeDurableMemory) Clear(context.Context, string, string) error {
	m.clearCalls++
	if m.clearErr != nil {
		return m.clearErr
	}
	m.exists = false
	m.snapshot = Snapshot{}
	return nil
}

type fakeSnapshotCache struct {
	snapshot Snapshot
	set      bool

	loadErr   error
	storeErr  error
	deleteErr error
	clearErr  error

	loadCalls   int
	storeCalls  int
	deleteCalls int
	clearCalls  int
}

func (m *fakeSnapshotCache) LoadSnapshot(_ context.Context, _, _ string, expectedVersion int64, limit int) (Snapshot, bool, error) {
	m.loadCalls++
	if m.loadErr != nil {
		return Snapshot{}, false, m.loadErr
	}
	if !m.set || m.snapshot.Version != expectedVersion || m.snapshot.CachedLimit < limit {
		return Snapshot{}, false, nil
	}
	return cloneTestSnapshot(m.snapshot), true, nil
}

func (m *fakeSnapshotCache) StoreSnapshot(_ context.Context, _, _ string, snapshot Snapshot) error {
	m.storeCalls++
	if m.storeErr != nil {
		return m.storeErr
	}
	m.snapshot = cloneTestSnapshot(snapshot)
	m.set = true
	return nil
}

func (m *fakeSnapshotCache) DeleteSnapshot(context.Context, string, string) error {
	m.deleteCalls++
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.set = false
	m.snapshot = Snapshot{}
	return nil
}

func (m *fakeSnapshotCache) Clear(context.Context, string, string) error {
	m.clearCalls++
	if m.clearErr != nil {
		return m.clearErr
	}
	m.set = false
	m.snapshot = Snapshot{}
	return nil
}

func cloneTestSnapshot(snapshot Snapshot) Snapshot {
	data, _ := json.Marshal(snapshot)
	var clone Snapshot
	_ = json.Unmarshal(data, &clone)
	return clone
}

func cloneTestMessage(msg *schema.Message) *schema.Message {
	data, _ := json.Marshal(msg)
	var clone schema.Message
	_ = json.Unmarshal(data, &clone)
	return &clone
}
