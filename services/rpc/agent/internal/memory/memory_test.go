package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/eino/schema"
	goredis "github.com/redis/go-redis/v9"
)

// newManagers 构造两种实现，让所有行为用例在 InMemory 与 Redis 上各跑一遍，保证语义一致。
func newManagers(t *testing.T, conf Conf) map[string]Manager {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return map[string]Manager{
		"inmemory": NewInMemory(conf),
		"redis":    NewRedis(client, conf),
	}
}

// TestManagerRoundTrip 验证消息（含中文与 ToolCalls）写入后按时间正序完整读回。
func TestManagerRoundTrip(t *testing.T) {
	ctx := context.Background()
	toolMsg := schema.AssistantMessage("", []schema.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: schema.FunctionCall{Name: "search_products", Arguments: `{"query":"键盘"}`},
	}})

	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			if err := m.Append(ctx, "u1", "c1", schema.UserMessage("预算3000买学习用品"), schema.AssistantMessage("已选3件商品", nil)); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			if err := m.Append(ctx, "u1", "c1", toolMsg); err != nil {
				t.Fatalf("Append(toolMsg) error = %v", err)
			}

			got, err := m.History(ctx, "u1", "c1", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(got) != 3 {
				t.Fatalf("expected 3 messages, got %d", len(got))
			}
			if got[0].Role != schema.User || got[0].Content != "预算3000买学习用品" {
				t.Fatalf("unexpected first message: %+v", got[0])
			}
			if got[1].Role != schema.Assistant || got[1].Content != "已选3件商品" {
				t.Fatalf("unexpected second message: %+v", got[1])
			}
			if len(got[2].ToolCalls) != 1 || got[2].ToolCalls[0].Function.Name != "search_products" {
				t.Fatalf("tool calls not round-tripped: %+v", got[2])
			}
		})
	}
}

// TestManagerDeepCopy 验证修改 History 返回值不影响存储内容（回归 agent-demo 的指针共享 bug）。
func TestManagerDeepCopy(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			if err := m.Append(ctx, "u1", "c1", schema.UserMessage("原始内容")); err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			first, err := m.History(ctx, "u1", "c1", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			first[0].Content = "被调用方改写"

			again, err := m.History(ctx, "u1", "c1", 0)
			if err != nil {
				t.Fatalf("History() again error = %v", err)
			}
			if again[0].Content != "原始内容" {
				t.Fatalf("stored message mutated via returned pointer: %q", again[0].Content)
			}
		})
	}
}

// TestManagerWindow 验证窗口截断切在问答对边界：MaxHistory=4 时保留最近 2 对。
func TestManagerWindow(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{MaxHistory: 5}) { // 5 归一化为 4
		t.Run(name, func(t *testing.T) {
			for i, q := range []string{"第一轮", "第二轮", "第三轮"} {
				if err := m.Append(ctx, "u1", "c1",
					schema.UserMessage(q),
					schema.AssistantMessage("回答"+q, nil)); err != nil {
					t.Fatalf("Append(round %d) error = %v", i, err)
				}
			}

			got, err := m.History(ctx, "u1", "c1", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(got) != 4 {
				t.Fatalf("expected window of 4, got %d", len(got))
			}
			if got[0].Role != schema.User || got[0].Content != "第二轮" {
				t.Fatalf("window not aligned to qa pair, first = %+v", got[0])
			}
			if got[3].Content != "回答第三轮" {
				t.Fatalf("unexpected last message: %+v", got[3])
			}
		})
	}
}

// TestManagerMissingConversation 验证不存在的会话返回空切片而非错误。
func TestManagerMissingConversation(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			got, err := m.History(ctx, "u1", "ghost", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("expected empty history, got %d", len(got))
			}
		})
	}
}

// TestManagerStableTitle 验证标题独立于滚动消息窗口，始终保留首次候选值。
func TestManagerStableTitle(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{MaxHistory: 2}) {
		t.Run(name, func(t *testing.T) {
			first, err := m.GetOrCreateTitle(ctx, "u1", "c1", "第一轮标题")
			if err != nil {
				t.Fatalf("GetOrCreateTitle() error = %v", err)
			}
			if first != "第一轮标题" {
				t.Fatalf("unexpected first title %q", first)
			}

			for _, q := range []string{"第一轮", "第二轮", "第三轮"} {
				if err := m.Append(ctx, "u1", "c1", schema.UserMessage(q), schema.AssistantMessage("回答"+q, nil)); err != nil {
					t.Fatalf("Append() error = %v", err)
				}
			}

			again, err := m.GetOrCreateTitle(ctx, "u1", "c1", "第三轮标题")
			if err != nil {
				t.Fatalf("GetOrCreateTitle() again error = %v", err)
			}
			if again != "第一轮标题" {
				t.Fatalf("title changed after history truncation: %q", again)
			}
		})
	}
}

func TestManagerUserIsolation(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			if err := m.Append(ctx, "user-a", "c1", schema.UserMessage("only user a can read this")); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			got, err := m.History(ctx, "user-b", "c1", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("history leaked across users: %+v", got)
			}
		})
	}
}

func TestManagerClear(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			if _, err := m.GetOrCreateTitle(ctx, "u1", "c1", "旧标题"); err != nil {
				t.Fatalf("GetOrCreateTitle() error = %v", err)
			}
			if err := m.Append(ctx, "u1", "c1", schema.UserMessage("hi")); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			if err := m.Clear(ctx, "u1", "c1"); err != nil {
				t.Fatalf("Clear() error = %v", err)
			}
			got, err := m.History(ctx, "u1", "c1", 0)
			if err != nil {
				t.Fatalf("History() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("expected empty history after clear, got %d", len(got))
			}
			title, err := m.GetOrCreateTitle(ctx, "u1", "c1", "新标题")
			if err != nil {
				t.Fatalf("GetOrCreateTitle() after clear error = %v", err)
			}
			if title != "新标题" {
				t.Fatalf("expected title to be cleared, got %q", title)
			}
		})
	}
}

// TestManagerEmptyConversationId 验证空会话 ID 直接报错，避免写进共享的空 key。
func TestManagerEmptyConversationId(t *testing.T) {
	ctx := context.Background()
	for name, m := range newManagers(t, Conf{}) {
		t.Run(name, func(t *testing.T) {
			if err := m.Append(ctx, "u1", "", schema.UserMessage("hi")); err == nil {
				t.Fatal("expected error for empty conversation id")
			}
		})
	}
}

// TestRedisTTL 验证 Redis 实现设置了滑动 TTL，空闲超时后会话过期。
func TestRedisTTL(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	m := NewRedis(client, Conf{TTL: time.Minute})
	if _, err := m.GetOrCreateTitle(ctx, "u1", "c1", "稳定标题"); err != nil {
		t.Fatalf("GetOrCreateTitle() error = %v", err)
	}
	if err := m.Append(ctx, "u1", "c1", schema.UserMessage("hi")); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if ttl := mr.TTL(convKey("u1", "c1")); ttl != time.Minute {
		t.Fatalf("expected ttl 1m, got %v", ttl)
	}
	if ttl := mr.TTL(titleKey("u1", "c1")); ttl != time.Minute {
		t.Fatalf("expected title ttl 1m, got %v", ttl)
	}
	mr.FastForward(30 * time.Second)
	if err := m.Append(ctx, "u1", "c1", schema.AssistantMessage("still active", nil)); err != nil {
		t.Fatalf("Append() refresh error = %v", err)
	}
	if ttl := mr.TTL(convKey("u1", "c1")); ttl != time.Minute {
		t.Fatalf("expected refreshed ttl 1m, got %v", ttl)
	}
	if ttl := mr.TTL(titleKey("u1", "c1")); ttl != time.Minute {
		t.Fatalf("expected refreshed title ttl 1m, got %v", ttl)
	}

	mr.FastForward(2 * time.Minute)
	got, err := m.History(ctx, "u1", "c1", 0)
	if err != nil {
		t.Fatalf("History() after expire error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected expired conversation to be empty, got %d", len(got))
	}
	if mr.Exists(titleKey("u1", "c1")) {
		t.Fatal("expected expired conversation title to be removed")
	}
}

// TestConfNormalize 验证配置归一化：默认值回填、窗口取偶。
func TestConfNormalize(t *testing.T) {
	got := Conf{}.normalize()
	if got.MaxHistory != defaultMaxHistory || got.TTL != defaultTTL {
		t.Fatalf("zero conf not normalized to defaults: %+v", got)
	}
	if got := (Conf{MaxHistory: 7}).normalize(); got.MaxHistory != 6 {
		t.Fatalf("odd window should round down to even, got %d", got.MaxHistory)
	}
	if got := (Conf{MaxHistory: 1}).normalize(); got.MaxHistory != 2 {
		t.Fatalf("window below 2 should clamp to 2, got %d", got.MaxHistory)
	}
}
