package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// InMemory 是 Manager 的进程内实现，用于未配置 Redis 的本地开发与单元测试。
// 消息以 JSON 编码字节存储，读取时解码出全新对象，与 Redis 实现语义一致；
// 创建标题或追加消息时刷新滑动 TTL，后续读写会惰性清理过期会话。
// 多实例部署下会话彼此不可见，生产环境仍应使用 Redis 实现。
type InMemory struct {
	conf Conf
	now  func() time.Time

	mu sync.RWMutex
	// conv 以 userID:conversationID 为键，确保内存降级与 Redis 使用相同的隔离语义。
	conv        map[string][][]byte
	titles      map[string]string
	expiresAt   map[string]time.Time
	nextCleanup time.Time
}

// 确保 InMemory 实现 Manager。
var _ Manager = (*InMemory)(nil)

// NewInMemory 创建进程内会话记忆。
func NewInMemory(c Conf) *InMemory {
	return newInMemory(c, time.Now)
}

// newInMemory 支持注入时钟，便于无等待地验证 TTL 行为。
func newInMemory(c Conf, now func() time.Time) *InMemory {
	conf := c.normalize()
	if now == nil {
		now = time.Now
	}
	return &InMemory{
		conf:        conf,
		now:         now,
		conv:        make(map[string][][]byte),
		titles:      make(map[string]string),
		expiresAt:   make(map[string]time.Time),
		nextCleanup: now().Add(inMemoryCleanupInterval(conf.TTL)),
	}
}

// Append 追加消息并按窗口截断头部，会话不存在时自动创建。
func (m *InMemory) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	_ = ctx
	if userId == "" || conversationId == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	if len(msgs) == 0 {
		return nil
	}

	now := m.now()
	encoded := make([][]byte, 0, len(msgs))
	for _, msg := range msgs {
		data, err := encodeMessage(msg, now)
		if err != nil {
			return err
		}
		encoded = append(encoded, data)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	key := conversationKey(userId, conversationId)
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	list := append(m.conv[key], encoded...)
	if excess := len(list) - m.conf.MaxHistory; excess > 0 {
		list = list[excess:]
	}
	m.conv[key] = list
	m.expiresAt[key] = now.Add(m.conf.TTL)
	return nil
}

// History 返回最近 limit 条消息（时间正序）；limit 非正时使用窗口大小。
func (m *InMemory) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	_ = ctx
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}

	now := m.now()
	key := conversationKey(userId, conversationId)
	m.mu.Lock()
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	list := m.conv[key]
	if len(list) > limit {
		list = list[len(list)-limit:]
	}
	// 拷贝切片头即可：存储字节只写不改，解码在锁外进行。
	snapshot := append([][]byte(nil), list...)
	m.mu.Unlock()

	out := make([]*schema.Message, 0, len(snapshot))
	for _, data := range snapshot {
		msg, err := decodeMessage(data)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

// GetOrCreateTitle 保存并返回会话的首个候选标题。
func (m *InMemory) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error) {
	_ = ctx
	if userId == "" || conversationId == "" {
		return "", fmt.Errorf("memory: user id or conversation id is empty")
	}

	key := conversationKey(userId, conversationId)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	if title, ok := m.titles[key]; ok {
		return title, nil
	}
	m.titles[key] = candidate
	m.expiresAt[key] = now.Add(m.conf.TTL)
	return candidate, nil
}

// Clear 删除会话的全部历史与标题。
func (m *InMemory) Clear(ctx context.Context, userId, conversationId string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	key := conversationKey(userId, conversationId)
	m.deleteLocked(key)
	return nil
}

// cleanupExpiredLocked 按固定间隔扫描全部会话，避免一次性会话永久占用内存。
func (m *InMemory) cleanupExpiredLocked(now time.Time) {
	if now.Before(m.nextCleanup) {
		return
	}
	for key, expiresAt := range m.expiresAt {
		if !now.Before(expiresAt) {
			m.deleteLocked(key)
		}
	}
	m.nextCleanup = now.Add(inMemoryCleanupInterval(m.conf.TTL))
}

// deleteIfExpiredLocked 清理当前访问的过期会话，保证读取不会返回过期数据。
func (m *InMemory) deleteIfExpiredLocked(key string, now time.Time) {
	if expiresAt, ok := m.expiresAt[key]; ok && !now.Before(expiresAt) {
		m.deleteLocked(key)
	}
}

// deleteLocked 原子删除一个会话的消息、标题和过期状态。
func (m *InMemory) deleteLocked(key string) {
	delete(m.conv, key)
	delete(m.titles, key)
	delete(m.expiresAt, key)
}

// inMemoryCleanupInterval 限制全局扫描频率；短 TTL 测试与长 TTL 默认配置都能及时回收。
func inMemoryCleanupInterval(ttl time.Duration) time.Duration {
	const maxInterval = time.Minute
	if ttl < maxInterval {
		return ttl
	}
	return maxInterval
}
