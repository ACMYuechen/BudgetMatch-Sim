package recommend

import (
	"context"
	"sync"
)

// conversationLockKey 按认证用户与会话共同定位串行化范围。
type conversationLockKey struct {
	userId         string
	conversationId string
}

// conversationLock 是容量为 1 的可取消信号量；refs 包含持有者与等待者。
type conversationLock struct {
	token chan struct{}
	refs  int
}

// conversationLocker 在单个 agent-rpc 实例内串行化同一用户的同一会话请求。
// 不同用户或不同会话使用不同条目，仍可并发执行。
type conversationLocker struct {
	mu      sync.Mutex
	entries map[conversationLockKey]*conversationLock
}

func newConversationLocker() *conversationLocker {
	return &conversationLocker{entries: make(map[conversationLockKey]*conversationLock)}
}

// acquire 等待获得会话执行权；等待期间响应 context 取消。
// 返回的 release 可重复调用，但只会释放一次。
func (l *conversationLocker) acquire(ctx context.Context, key conversationLockKey) (release func(), err error) {
	entry := l.reference(key)
	select {
	case <-entry.token:
		var once sync.Once
		return func() {
			once.Do(func() {
				entry.token <- struct{}{}
				l.unreference(key, entry)
			})
		}, nil
	case <-ctx.Done():
		l.unreference(key, entry)
		return nil, ctx.Err()
	}
}

// reference 获取或创建锁条目，并登记一个持有者或等待者引用。
func (l *conversationLocker) reference(key conversationLockKey) *conversationLock {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry == nil {
		entry = &conversationLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		l.entries[key] = entry
	}
	entry.refs++
	return entry
}

// unreference 释放引用，并在无人持有或等待时回收锁条目。
func (l *conversationLocker) unreference(key conversationLockKey, entry *conversationLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && l.entries[key] == entry {
		delete(l.entries, key)
	}
}
