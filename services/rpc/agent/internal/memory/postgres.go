package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Postgres 是会话与轮次的长期事实源。
type Postgres struct {
	conf  Conf
	db    *gorm.DB
	model conversation_memory.ConversationMemoryModel
	now   func() time.Time
}

var _ ConversationStore = (*Postgres)(nil)

// postgresConnectionContextKey 用于把持有 advisory lock 的专用连接传递给回调内查询。
type postgresConnectionContextKey struct{}

// NewPostgres 创建 PostgreSQL 长期记忆实现；建表与结构检查由服务启动流程显式调用。
func NewPostgres(db *gorm.DB, c Conf) *Postgres {
	return &Postgres{conf: c.normalize(), db: db, model: conversation_memory.NewConversationMemoryModel(db), now: time.Now}
}

// CreateTable 使用项目统一的 GORM model 创建或补齐会话与轮次表。
func (m *Postgres) CreateTable() error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	if err := m.model.CreateTable(); err != nil {
		return fmt.Errorf("memory: create postgres tables: %w", err)
	}
	return nil
}

// CheckSchema 检查运行所需的表、列、索引和级联外键是否齐全。
func (m *Postgres) CheckSchema() error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	if err := m.model.CheckSchema(); err != nil {
		return fmt.Errorf("memory: check postgres schema: %w", err)
	}
	return nil
}

// WithConversationLock 使用连接级 PostgreSQL advisory lock 串行化跨实例的同一会话。
// 回调中的所有 model 操作通过 context 复用持锁连接，避免额外占用第二条连接。
func (m *Postgres) WithConversationLock(ctx context.Context, userId, conversationId string, fn func(context.Context) error) error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	lockKey := conversationKey(userId, conversationId)
	return m.db.WithContext(ctx).Connection(func(conn *gorm.DB) (callbackErr error) {
		if err := conn.Exec(`SELECT pg_advisory_lock(hashtextextended(?, 0))`, lockKey).Error; err != nil {
			return fmt.Errorf("memory: acquire postgres conversation lock: %w", err)
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			releaseErr := conn.WithContext(releaseCtx).
				Exec(`SELECT pg_advisory_unlock(hashtextextended(?, 0))`, lockKey).Error
			if callbackErr == nil && releaseErr != nil {
				callbackErr = fmt.Errorf("memory: release postgres conversation lock: %w", releaseErr)
			}
		}()
		lockedCtx := context.WithValue(ctx, postgresConnectionContextKey{}, conn)
		return fn(lockedCtx)
	})
}

// GetConversation 读取会话元数据，并把数据库 JSONB 状态解码为领域模型。
func (m *Postgres) GetConversation(ctx context.Context, userId, conversationId string) (Conversation, bool, error) {
	if !m.available() {
		return Conversation{}, false, fmt.Errorf("memory: postgres database is nil")
	}
	stored, exists, err := m.modelFor(ctx).GetConversation(ctx, userId, conversationId)
	if err != nil || !exists {
		return Conversation{}, exists, wrapPostgres("get conversation", err)
	}
	conversation, err := decodeConversation(stored)
	return conversation, true, err
}

// FindTurn 按复合主键读取已完成轮次，供幂等请求重放完整结果。
func (m *Postgres) FindTurn(ctx context.Context, userId, conversationId, turnId string) (Turn, bool, error) {
	if !m.available() {
		return Turn{}, false, fmt.Errorf("memory: postgres database is nil")
	}
	stored, exists, err := m.modelFor(ctx).FindTurn(ctx, userId, conversationId, turnId)
	if err != nil || !exists {
		return Turn{}, exists, wrapPostgres("find turn", err)
	}
	turn, err := decodeTurn(stored)
	return turn, true, err
}

// SaveTurn 编码结构化状态并委托 model 在同一事务中保存会话与轮次。
func (m *Postgres) SaveTurn(ctx context.Context, req SaveTurnReq) (Conversation, Turn, error) {
	if req.UserId == "" || req.ConversationId == "" || req.TurnId == "" {
		return Conversation{}, Turn{}, fmt.Errorf("memory: user id, conversation id or turn id is empty")
	}
	if !m.available() {
		return Conversation{}, Turn{}, fmt.Errorf("memory: postgres database is nil")
	}
	if req.Now.IsZero() {
		req.Now = m.now()
	}
	stateJSON, err := json.Marshal(req.Intent)
	if err != nil {
		return Conversation{}, Turn{}, fmt.Errorf("memory: encode conversation state: %w", err)
	}
	if !json.Valid(req.ResultJSON) {
		return Conversation{}, Turn{}, fmt.Errorf("memory: result is not valid JSON")
	}
	storedConversation, storedTurn, err := m.modelFor(ctx).SaveTurn(ctx, conversation_memory.SaveTurnReq{
		UserId: req.UserId, ConversationId: req.ConversationId, TurnId: req.TurnId,
		Title: req.Title, State: string(stateJSON), Query: req.Query,
		BudgetCents: req.BudgetCents, MaxItems: req.MaxItems, Intent: string(stateJSON),
		Result: string(req.ResultJSON), Summary: req.Summary, Now: req.Now,
	})
	if err != nil {
		return Conversation{}, Turn{}, fmt.Errorf("memory: save postgres turn: %w", err)
	}
	conversation, err := decodeConversation(storedConversation)
	if err != nil {
		return Conversation{}, Turn{}, err
	}
	turn, err := decodeTurn(storedTurn)
	return conversation, turn, err
}

// ListConversations 按最近更新时间倒序分页读取当前用户的长期会话。
func (m *Postgres) ListConversations(ctx context.Context, userId string, page, pageSize int) ([]Conversation, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	stored, total, err := m.modelFor(ctx).ListConversations(ctx, userId, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, wrapPostgres("list conversations", err)
	}
	conversations := make([]Conversation, 0, len(stored))
	for _, item := range stored {
		conversation, err := decodeConversation(item)
		if err != nil {
			return nil, 0, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, total, nil
}

// ListTurns 按 sequence 正序分页读取完整轮次，并同时返回会话最新状态。
func (m *Postgres) ListTurns(ctx context.Context, userId, conversationId string, page, pageSize int) (Conversation, []Turn, int64, bool, error) {
	page, pageSize = normalizePage(page, pageSize)
	storedConversation, storedTurns, total, exists, err := m.modelFor(ctx).ListTurns(ctx, userId, conversationId, (page-1)*pageSize, pageSize)
	if err != nil || !exists {
		return Conversation{}, nil, total, exists, wrapPostgres("list turns", err)
	}
	conversation, err := decodeConversation(storedConversation)
	if err != nil {
		return Conversation{}, nil, 0, true, err
	}
	turns := make([]Turn, 0, len(storedTurns))
	for _, stored := range storedTurns {
		turn, err := decodeTurn(stored)
		if err != nil {
			return Conversation{}, nil, 0, true, err
		}
		turns = append(turns, turn)
	}
	return conversation, turns, total, true, nil
}

// DeleteConversation 删除会话；轮次通过数据库复合外键级联删除。
func (m *Postgres) DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error) {
	deleted, err := m.modelFor(ctx).DeleteConversation(ctx, userId, conversationId)
	return deleted, wrapPostgres("delete conversation", err)
}

// Append 是旧消息接口的兼容适配；新推荐流程使用 SaveTurn 持久化完整轮次。
func (m *Postgres) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	query, summary := legacyMessages(msgs)
	resultJSON, _ := json.Marshal(map[string]any{
		"intent": map[string]any{"budget_cents": 0, "max_items": 0, "keywords": []string{}, "preferences": []string{}},
		"items":  []any{}, "total_price_cents": 0, "summary": summary, "tools_used": []any{},
		"conversation_id": conversationId,
	})
	_, _, err := m.SaveTurn(ctx, SaveTurnReq{
		UserId: userId, ConversationId: conversationId, TurnId: uuid.NewString(),
		Query: query, Summary: summary, ResultJSON: resultJSON, Now: m.now(),
	})
	return err
}

// History 从完整轮次中恢复最近的用户/助手消息窗口，供 LLM 构建短期上下文。
func (m *Postgres) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}
	conversation, exists, err := m.GetConversation(ctx, userId, conversationId)
	if err != nil || !exists {
		return []*schema.Message{}, err
	}
	turnLimit := (limit + 1) / 2
	offset := max(int(conversation.TurnCount)-turnLimit, 0)
	_, storedTurns, _, exists, err := m.modelFor(ctx).ListTurns(ctx, userId, conversationId, offset, turnLimit)
	if err != nil {
		return nil, wrapPostgres("read recent conversation turns", err)
	}
	if !exists {
		return []*schema.Message{}, nil
	}
	turns := make([]Turn, 0, len(storedTurns))
	for _, stored := range storedTurns {
		turn, decodeErr := decodeTurn(stored)
		if decodeErr != nil {
			return nil, decodeErr
		}
		turns = append(turns, turn)
	}
	messages := turnsToMessages(turns)
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// Version 返回会话版本，Tiered 使用它判断 Redis 快照是否仍然有效。
func (m *Postgres) Version(ctx context.Context, userId, conversationId string) (int64, bool, error) {
	if !m.available() {
		return 0, false, fmt.Errorf("memory: postgres database is nil")
	}
	version, exists, err := m.modelFor(ctx).Version(ctx, userId, conversationId)
	return version, exists, wrapPostgres("read conversation version", err)
}

// LoadSnapshot 组合稳定标题、版本和最近消息，作为 Redis 两级缓存的回填数据。
func (m *Postgres) LoadSnapshot(ctx context.Context, userId, conversationId string, limit int) (Snapshot, bool, error) {
	conversation, exists, err := m.GetConversation(ctx, userId, conversationId)
	if err != nil || !exists {
		return Snapshot{}, exists, err
	}
	messages, err := m.History(ctx, userId, conversationId, limit)
	if err != nil {
		return Snapshot{}, false, err
	}
	return Snapshot{Version: conversation.Version, CachedLimit: limit, Title: conversation.Title, TitleInitialized: true, Messages: messages}, true, nil
}

// GetOrCreateTitle 兼容旧 Manager 接口，并保证首个会话标题不会被后续请求覆盖。
func (m *Postgres) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error) {
	if userId == "" || conversationId == "" {
		return "", fmt.Errorf("memory: user id or conversation id is empty")
	}
	title, err := m.modelFor(ctx).GetOrCreateTitle(ctx, userId, conversationId, candidate, m.now())
	return title, wrapPostgres("get or create title", err)
}

// Clear 兼容旧 Manager 接口，语义等同于删除完整会话。
func (m *Postgres) Clear(ctx context.Context, userId, conversationId string) error {
	_, err := m.DeleteConversation(ctx, userId, conversationId)
	return err
}

// available 判断长期记忆是否已注入可用的数据库和 model。
func (m *Postgres) available() bool { return m != nil && m.db != nil && m.model != nil }

// modelFor 优先返回 context 中持锁连接绑定的 model，防止回调另取连接导致锁失效。
func (m *Postgres) modelFor(ctx context.Context) conversation_memory.ConversationMemoryModel {
	if conn, ok := ctx.Value(postgresConnectionContextKey{}).(*gorm.DB); ok && conn != nil {
		return conversation_memory.NewConversationMemoryModel(conn)
	}
	return m.model
}

// decodeConversation 将数据库模型转换为不依赖 GORM 的存储领域对象。
func decodeConversation(stored conversation_memory.AgentConversation) (Conversation, error) {
	var state IntentState
	if strings.TrimSpace(string(stored.State)) != "" && string(stored.State) != `{}` {
		if err := json.Unmarshal(stored.State, &state); err != nil {
			return Conversation{}, fmt.Errorf("memory: decode conversation state: %w", err)
		}
	}
	return Conversation{UserId: stored.UserId, ConversationId: stored.ConversationId, Title: stored.Title,
		State: state, Version: stored.Version, TurnCount: stored.TurnCount,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt}, nil
}

// decodeTurn 解码 JSONB 意图，并复制结果字节以隔离数据库返回缓冲区。
func decodeTurn(stored conversation_memory.AgentConversationTurn) (Turn, error) {
	var intent IntentState
	if err := json.Unmarshal(stored.Intent, &intent); err != nil {
		return Turn{}, fmt.Errorf("memory: decode turn intent: %w", err)
	}
	completedAt := time.Time{}
	if stored.CompletedAt != nil {
		completedAt = *stored.CompletedAt
	}
	return Turn{UserId: stored.UserId, ConversationId: stored.ConversationId, TurnId: stored.TurnId,
		Sequence: stored.Sequence, Status: stored.Status, Query: stored.Query,
		BudgetCents: stored.BudgetCents, MaxItems: stored.MaxItems, Intent: intent,
		ResultJSON: append(json.RawMessage(nil), stored.Result...), Summary: stored.Summary,
		CreatedAt: stored.CreatedAt, CompletedAt: completedAt}, nil
}

// wrapPostgres 为底层错误补充稳定的存储操作上下文。
func wrapPostgres(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("memory: %s in postgres: %w", operation, err)
}

// legacyMessages 从旧接口消息中提取最后一条用户请求与助手摘要。
func legacyMessages(messages []*schema.Message) (query, summary string) {
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.User {
			query = message.Content
		} else if message.Role == schema.Assistant {
			summary = message.Content
		}
	}
	return query, summary
}

// turnsToMessages 把持久化轮次还原为 Eino 所需的交替消息序列。
func turnsToMessages(turns []Turn) []*schema.Message {
	messages := make([]*schema.Message, 0, len(turns)*2)
	for _, turn := range turns {
		messages = append(messages, schema.UserMessage(turn.Query), schema.AssistantMessage(turn.Summary, nil))
	}
	return messages
}
