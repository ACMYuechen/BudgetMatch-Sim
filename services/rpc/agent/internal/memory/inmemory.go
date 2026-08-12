package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// InMemory 是 ConversationStore 的进程内实现，用于未配置外部存储的本地开发与单元测试。
// 消息以 JSON 编码字节存储，读取时解码出全新对象，与 Redis 实现语义一致；
// 创建标题或追加消息时刷新滑动 TTL，后续读写会惰性清理过期会话。
// 多实例部署下会话彼此不可见，生产环境应使用 Redis 或 PostgreSQL 实现。
type InMemory struct {
	conf Conf
	now  func() time.Time

	mu sync.RWMutex
	// conv 以 userID:conversationID 为键，确保内存降级与 Redis 使用相同的隔离语义。
	conv          map[string][][]byte
	titles        map[string]string
	conversations map[string]Conversation
	turns         map[string][]Turn
	expiresAt     map[string]time.Time
	nextCleanup   time.Time
}

// 确保 InMemory 实现包含完整会话能力的 ConversationStore。
var _ ConversationStore = (*InMemory)(nil)

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
		conf:          conf,
		now:           now,
		conv:          make(map[string][][]byte),
		titles:        make(map[string]string),
		conversations: make(map[string]Conversation),
		turns:         make(map[string][]Turn),
		expiresAt:     make(map[string]time.Time),
		nextCleanup:   now().Add(inMemoryCleanupInterval(conf.TTL)),
	}
}

// WithConversationLock 由 Service 的进程内锁负责串行化；InMemory 不需要第二把锁。
func (m *InMemory) WithConversationLock(ctx context.Context, _, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

// GetConversation 返回未过期的会话元数据，并保证结果与内部存储不共享切片。
func (m *InMemory) GetConversation(_ context.Context, userId, conversationId string) (Conversation, bool, error) {
	key := conversationKey(userId, conversationId)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	conversation, exists := m.conversations[key]
	return cloneConversation(conversation), exists, nil
}

// FindTurn 按 turn_id 查找已完成轮次，供请求重试时直接复用原结果。
func (m *InMemory) FindTurn(_ context.Context, userId, conversationId, turnId string) (Turn, bool, error) {
	key := conversationKey(userId, conversationId)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	for _, turn := range m.turns[key] {
		if turn.TurnId == turnId {
			return cloneTurn(turn), true, nil
		}
	}
	return Turn{}, false, nil
}

// SaveTurn 原子更新会话状态、完整轮次和供模型使用的滚动消息窗口。
// 相同 turn_id 已存在时不会覆盖结果或增加轮次数。
func (m *InMemory) SaveTurn(_ context.Context, req SaveTurnReq) (Conversation, Turn, error) {
	if req.UserId == "" || req.ConversationId == "" || req.TurnId == "" {
		return Conversation{}, Turn{}, fmt.Errorf("memory: user id, conversation id or turn id is empty")
	}
	if !json.Valid(req.ResultJSON) {
		return Conversation{}, Turn{}, fmt.Errorf("memory: result is not valid JSON")
	}
	if req.Now.IsZero() {
		req.Now = m.now()
	}
	key := conversationKey(req.UserId, req.ConversationId)
	userMessage, err := encodeMessage(schema.UserMessage(req.Query), req.Now)
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	assistantMessage, err := encodeMessage(schema.AssistantMessage(req.Summary, nil), req.Now)
	if err != nil {
		return Conversation{}, Turn{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(req.Now)
	m.deleteIfExpiredLocked(key, req.Now)
	for _, turn := range m.turns[key] {
		if turn.TurnId == req.TurnId {
			return cloneConversation(m.conversations[key]), cloneTurn(turn), nil
		}
	}
	conversation, exists := m.conversations[key]
	if !exists {
		conversation = Conversation{UserId: req.UserId, ConversationId: req.ConversationId,
			Title: req.Title, CreatedAt: req.Now}
	}
	if conversation.Title == "" {
		conversation.Title = req.Title
	}
	conversation.State = req.Intent
	conversation.Version++
	conversation.TurnCount++
	conversation.UpdatedAt = req.Now
	turn := Turn{UserId: req.UserId, ConversationId: req.ConversationId, TurnId: req.TurnId,
		Sequence: conversation.TurnCount, Status: TurnStatusCompleted, Query: req.Query,
		BudgetCents: req.BudgetCents, MaxItems: req.MaxItems, Intent: req.Intent,
		ResultJSON: append(json.RawMessage(nil), req.ResultJSON...), Summary: req.Summary,
		CreatedAt: req.Now, CompletedAt: req.Now}
	m.conversations[key] = conversation
	m.turns[key] = append(m.turns[key], turn)
	m.titles[key] = conversation.Title
	list := append(m.conv[key], userMessage, assistantMessage)
	if excess := len(list) - m.conf.MaxHistory; excess > 0 {
		list = list[excess:]
	}
	m.conv[key] = list
	m.expiresAt[key] = req.Now.Add(m.conf.TTL)
	return cloneConversation(conversation), cloneTurn(turn), nil
}

// ListConversations 按最近更新时间倒序返回指定用户的未过期会话。
func (m *InMemory) ListConversations(_ context.Context, userId string, page, pageSize int) ([]Conversation, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	items := make([]Conversation, 0)
	for _, conversation := range m.conversations {
		if conversation.UserId == userId {
			items = append(items, cloneConversation(conversation))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	total := int64(len(items))
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []Conversation{}, total, nil
	}
	end := min(start+pageSize, len(items))
	return items[start:end], total, nil
}

// ListTurns 按序号正序分页返回一个会话的完整轮次。
func (m *InMemory) ListTurns(_ context.Context, userId, conversationId string, page, pageSize int) (Conversation, []Turn, int64, bool, error) {
	page, pageSize = normalizePage(page, pageSize)
	key := conversationKey(userId, conversationId)
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)
	m.deleteIfExpiredLocked(key, now)
	conversation, exists := m.conversations[key]
	if !exists {
		return Conversation{}, []Turn{}, 0, false, nil
	}
	stored := m.turns[key]
	total := int64(len(stored))
	start := (page - 1) * pageSize
	if start >= len(stored) {
		return cloneConversation(conversation), []Turn{}, total, true, nil
	}
	end := min(start+pageSize, len(stored))
	turns := make([]Turn, 0, end-start)
	for _, turn := range stored[start:end] {
		turns = append(turns, cloneTurn(turn))
	}
	return cloneConversation(conversation), turns, total, true, nil
}

// DeleteConversation 删除会话元数据、轮次和兼容消息窗口。
func (m *InMemory) DeleteConversation(_ context.Context, userId, conversationId string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := conversationKey(userId, conversationId)
	_, exists := m.conversations[key]
	m.deleteLocked(key)
	return exists, nil
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
	m.conversations[key] = Conversation{UserId: userId, ConversationId: conversationId, Title: candidate,
		CreatedAt: now, UpdatedAt: now}
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
	delete(m.conversations, key)
	delete(m.turns, key)
}

// inMemoryCleanupInterval 限制全局扫描频率；短 TTL 测试与长 TTL 默认配置都能及时回收。
func inMemoryCleanupInterval(ttl time.Duration) time.Duration {
	const maxInterval = time.Minute
	if ttl < maxInterval {
		return ttl
	}
	return maxInterval
}
