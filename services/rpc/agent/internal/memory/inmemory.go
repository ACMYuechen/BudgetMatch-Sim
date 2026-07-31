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
// 不模拟 TTL 过期，多实例部署下会话彼此不可见，生产环境应使用 Redis 实现。
type InMemory struct {
	conf Conf

	mu   sync.RWMutex
	conv map[string][][]byte
}

// 确保 InMemory 实现 Manager。
var _ Manager = (*InMemory)(nil)

// NewInMemory 创建进程内会话记忆。
func NewInMemory(c Conf) *InMemory {
	return &InMemory{
		conf: c.normalize(),
		conv: make(map[string][][]byte),
	}
}

// Append 追加消息并按窗口截断头部，会话不存在时自动创建。
func (m *InMemory) Append(ctx context.Context, userID, conversationID string, msgs ...*schema.Message) error {
	_ = ctx
	if userID == "" || conversationID == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	if len(msgs) == 0 {
		return nil
	}

	now := time.Now()
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
	list := append(m.conv[conversationKey(userID, conversationID)], encoded...)
	if excess := len(list) - m.conf.MaxHistory; excess > 0 {
		list = list[excess:]
	}
	m.conv[conversationKey(userID, conversationID)] = list
	return nil
}

// History 返回最近 limit 条消息（时间正序）；limit 非正时使用窗口大小。
func (m *InMemory) History(ctx context.Context, userID, conversationID string, limit int) ([]*schema.Message, error) {
	_ = ctx
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}

	m.mu.RLock()
	list := m.conv[conversationKey(userID, conversationID)]
	if len(list) > limit {
		list = list[len(list)-limit:]
	}
	// 拷贝切片头即可：存储字节只写不改，解码在锁外进行。
	snapshot := append([][]byte(nil), list...)
	m.mu.RUnlock()

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

// Clear 删除会话的全部历史。
func (m *InMemory) Clear(ctx context.Context, userID, conversationID string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.conv, conversationKey(userID, conversationID))
	return nil
}
