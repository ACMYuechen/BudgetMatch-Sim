package recommend

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	agentcore "budgetmatch-sim/services/rpc/agent/internal/agent"
	"budgetmatch-sim/services/rpc/agent/internal/memory"
)

// TestConversationLockerCancellation 验证同一会话等待期间响应 context 取消并回收锁条目。
func TestConversationLockerCancellation(t *testing.T) {
	locker := newConversationLocker()
	key := conversationLockKey{userId: "u1", conversationId: "c1"}
	release, err := locker.acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := locker.acquire(ctx, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquire() error = %v, want deadline exceeded", err)
	}
	release()

	locker.mu.Lock()
	entries := len(locker.entries)
	locker.mu.Unlock()
	if entries != 0 {
		t.Fatalf("lock entries leaked after cancellation: %d", entries)
	}
}

// TestConversationLockerAllowsDifferentKeys 验证不同用户或会话不会互相阻塞。
func TestConversationLockerAllowsDifferentKeys(t *testing.T) {
	locker := newConversationLocker()
	releaseFirst, err := locker.acquire(context.Background(), conversationLockKey{userId: "u1", conversationId: "c1"})
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer releaseFirst()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	releaseSecond, err := locker.acquire(ctx, conversationLockKey{userId: "u1", conversationId: "c2"})
	if err != nil {
		t.Fatalf("different conversation should not block: %v", err)
	}
	releaseSecond()

	releaseThird, err := locker.acquire(ctx, conversationLockKey{userId: "u2", conversationId: "c1"})
	if err != nil {
		t.Fatalf("different user should not block: %v", err)
	}
	releaseThird()
}

// TestServiceSerializesSameConversation 验证并发的同会话请求依次执行并按顺序写入历史。
func TestServiceSerializesSameConversation(t *testing.T) {
	agent := newGatedAgent()
	defer agent.release()
	mem := memory.NewInMemory(memory.Conf{})
	service := NewService(agent, nil, mem)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Recommend(context.Background(), agentcore.Input{
			Query:          "first",
			UserId:         "u1",
			ConversationId: "c1",
		})
		firstDone <- err
	}()
	if run := <-agent.runs; run != "first" {
		t.Fatalf("unexpected first run %q", run)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := service.Recommend(context.Background(), agentcore.Input{
			Query:          "second",
			UserId:         "u1",
			ConversationId: "c1",
		})
		secondDone <- err
	}()
	waitForLockRefs(t, service.locks, conversationLockKey{userId: "u1", conversationId: "c1"}, 2)

	agent.release()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Recommend() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Recommend() error = %v", err)
	}

	history, err := mem.History(context.Background(), "u1", "c1", 0)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 4 ||
		history[0].Content != "first" || history[1].Content != "first" ||
		history[2].Content != "second" || history[3].Content != "second" {
		t.Fatalf("unexpected serialized history: %+v", history)
	}
}

// waitForLockRefs 等待测试请求进入锁队列，避免依赖固定 sleep。
func waitForLockRefs(t *testing.T, locker *conversationLocker, key conversationLockKey, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		locker.mu.Lock()
		entry := locker.entries[key]
		refs := 0
		if entry != nil {
			refs = entry.refs
		}
		locker.mu.Unlock()
		if refs >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("lock refs did not reach %d", want)
}

// TestServiceAllowsDifferentConversationsConcurrently 验证一个阻塞会话不影响另一个会话执行。
func TestServiceAllowsDifferentConversationsConcurrently(t *testing.T) {
	agent := newGatedAgent()
	defer agent.release()
	service := NewService(agent, nil, nil)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Recommend(context.Background(), agentcore.Input{
			Query:          "first",
			UserId:         "u1",
			ConversationId: "c1",
		})
		firstDone <- err
	}()
	if run := <-agent.runs; run != "first" {
		t.Fatalf("unexpected first run %q", run)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.Recommend(ctx, agentcore.Input{
		Query:          "second",
		UserId:         "u1",
		ConversationId: "c2",
	})
	if err != nil {
		t.Fatalf("different conversation was blocked: %v", err)
	}
	if result.Summary != "second" {
		t.Fatalf("unexpected second result: %+v", result)
	}

	agent.release()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Recommend() error = %v", err)
	}
}

// gatedAgent 仅阻塞 query=first 的调用，用于验证服务层并发边界。
type gatedAgent struct {
	runs         chan string
	releaseFirst chan struct{}
	releaseOnce  sync.Once
}

func (a *gatedAgent) release() {
	a.releaseOnce.Do(func() { close(a.releaseFirst) })
}

func newGatedAgent() *gatedAgent {
	return &gatedAgent{
		runs:         make(chan string, 4),
		releaseFirst: make(chan struct{}),
	}
}

func (a *gatedAgent) Name() string { return "gated" }

func (a *gatedAgent) Run(ctx context.Context, input agentcore.Input) (*agentcore.Result, error) {
	a.runs <- input.Query
	if input.Query == "first" {
		select {
		case <-a.releaseFirst:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &agentcore.Result{Summary: input.Query}, nil
}
