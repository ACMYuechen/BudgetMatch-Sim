package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	goredis "github.com/redis/go-redis/v9"
)

func TestRedisSnapshotRoundTrip(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	m := NewRedis(client, Conf{TTL: time.Minute})

	snapshot := Snapshot{
		Version:          3,
		CachedLimit:      4,
		Title:            "稳定标题",
		TitleInitialized: true,
		Messages: []*schema.Message{
			schema.UserMessage("第一轮"),
			schema.AssistantMessage("回答第一轮", nil),
		},
	}
	if err := m.StoreSnapshot(ctx, "u1", "c1", snapshot); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}
	if ttl := mr.TTL(snapshotKey("u1", "c1")); ttl != time.Minute {
		t.Fatalf("snapshot TTL = %v, want 1m", ttl)
	}

	got, hit, err := m.LoadSnapshot(ctx, "u1", "c1", 3, 2)
	if err != nil || !hit {
		t.Fatalf("LoadSnapshot() hit=%v err=%v", hit, err)
	}
	if got.Title != "稳定标题" || len(got.Messages) != 2 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	got.Messages[0].Content = "被调用方修改"
	again, hit, err := m.LoadSnapshot(ctx, "u1", "c1", 3, 2)
	if err != nil || !hit || again.Messages[0].Content != "第一轮" {
		t.Fatalf("Redis snapshot not deep-copied: %+v, hit=%v, err=%v", again, hit, err)
	}

	if _, hit, err := m.LoadSnapshot(ctx, "u1", "c1", 4, 2); err != nil || hit {
		t.Fatalf("version mismatch should miss, hit=%v err=%v", hit, err)
	}
	if _, hit, err := m.LoadSnapshot(ctx, "u1", "c1", 3, 5); err != nil || hit {
		t.Fatalf("insufficient cached window should miss, hit=%v err=%v", hit, err)
	}

	mr.FastForward(2 * time.Minute)
	if _, hit, err := m.LoadSnapshot(ctx, "u1", "c1", 3, 2); err != nil || hit {
		t.Fatalf("expired snapshot should miss, hit=%v err=%v", hit, err)
	}
}

func TestRedisSnapshotDecodeFailure(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	m := NewRedis(client, Conf{})

	mr.Set(snapshotKey("u1", "c1"), "not-json")
	if _, _, err := m.LoadSnapshot(ctx, "u1", "c1", 1, 1); err == nil {
		t.Fatal("expected corrupted snapshot error")
	}
}

func TestTieredRepairsCorruptedRedisSnapshot(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRedis(client, Conf{MaxHistory: 2})
	mr.Set(snapshotKey("u1", "c1"), "not-json")

	durable := &fakeDurableMemory{
		exists: true,
		snapshot: Snapshot{
			Version:  2,
			Messages: []*schema.Message{schema.UserMessage("数据库中的正确记忆")},
		},
	}
	m := newTiered(durable, cache, Conf{MaxHistory: 2})

	got, err := m.History(ctx, "u1", "c1", 2)
	if err != nil {
		t.Fatalf("History() should repair cache: %v", err)
	}
	if len(got) != 1 || got[0].Content != "数据库中的正确记忆" {
		t.Fatalf("unexpected repaired history: %+v", got)
	}
	if _, hit, err := cache.LoadSnapshot(ctx, "u1", "c1", 2, 2); err != nil || !hit {
		t.Fatalf("repaired snapshot hit=%v err=%v", hit, err)
	}
}
