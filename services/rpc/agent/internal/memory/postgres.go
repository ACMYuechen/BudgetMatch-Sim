package memory

import (
	"context"
	"fmt"
	"time"

	"budgetmatch-sim/services/rpc/agent/model/conversation_memory"

	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Postgres 是 Manager 的长期持久化实现。
//
// 会话元数据和完整消息历史由 conversation_memory model 持久化；本层只负责
// Manager 参数校验、Eino 消息编解码和最近历史窗口适配。
type Postgres struct {
	conf  Conf
	db    *gorm.DB
	model conversation_memory.ConversationMemoryModel
	now   func() time.Time
}

var _ Manager = (*Postgres)(nil)

// NewPostgres 基于已建立的 GORM PostgreSQL 连接创建持久化记忆。
func NewPostgres(db *gorm.DB, c Conf) *Postgres {
	return &Postgres{
		conf:  c.normalize(),
		db:    db,
		model: conversation_memory.NewConversationMemoryModel(db),
		now:   time.Now,
	}
}

// CreateTable 使用 GORM 幂等迁移会话记忆所需的表、索引和级联外键。
func (m *Postgres) CreateTable() error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	if err := m.model.CreateTable(); err != nil {
		return fmt.Errorf("memory: create postgres tables: %w", err)
	}
	return nil
}

// CheckSchema 在关闭 AutoMigrate 的部署中确认持久化结构已经由外部迁移创建。
func (m *Postgres) CheckSchema() error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	if err := m.model.CheckSchema(); err != nil {
		return fmt.Errorf("memory: check postgres schema: %w", err)
	}
	return nil
}

// Append 在一个事务中创建或刷新会话，并批量追加消息。
// PostgreSQL 保存全部消息，窗口限制只在 History 查询时应用。
func (m *Postgres) Append(ctx context.Context, userId, conversationId string, msgs ...*schema.Message) error {
	if userId == "" || conversationId == "" {
		return fmt.Errorf("memory: user id or conversation id is empty")
	}
	if len(msgs) == 0 {
		return nil
	}
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}

	now := m.now()
	encoded := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		data, err := encodeMessage(msg, now)
		if err != nil {
			return err
		}
		encoded = append(encoded, string(data))
	}

	err := m.model.Append(ctx, conversation_memory.AppendReq{
		UserId:         userId,
		ConversationId: conversationId,
		Messages:       encoded,
		Now:            now,
	})
	if err != nil {
		return fmt.Errorf("memory: append to postgres: %w", err)
	}
	return nil
}

// History 返回最近 limit 条消息并恢复为时间正序。
func (m *Postgres) History(ctx context.Context, userId, conversationId string, limit int) ([]*schema.Message, error) {
	if !m.available() {
		return nil, fmt.Errorf("memory: postgres database is nil")
	}
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}
	snapshot, exists, err := m.LoadSnapshot(ctx, userId, conversationId, limit)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []*schema.Message{}, nil
	}
	return snapshot.Messages, nil
}

// Version 返回会话持久化版本；会话不存在时 exists=false。
// 两级缓存每次命中前校验该值，防止数据库提交后 Redis 失效失败造成陈旧读取。
func (m *Postgres) Version(ctx context.Context, userId, conversationId string) (int64, bool, error) {
	if !m.available() {
		return 0, false, fmt.Errorf("memory: postgres database is nil")
	}
	version, exists, err := m.model.Version(ctx, userId, conversationId)
	if err != nil {
		return 0, false, fmt.Errorf("memory: read postgres conversation version: %w", err)
	}
	return version, exists, nil
}

// LoadSnapshot 从持久层的同一数据库快照恢复最近消息。
func (m *Postgres) LoadSnapshot(ctx context.Context, userId, conversationId string, limit int) (Snapshot, bool, error) {
	if !m.available() {
		return Snapshot{}, false, fmt.Errorf("memory: postgres database is nil")
	}
	if limit <= 0 {
		limit = m.conf.MaxHistory
	}

	stored, exists, err := m.model.LoadSnapshot(ctx, userId, conversationId, limit)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("memory: load postgres conversation snapshot: %w", err)
	}
	if !exists {
		return Snapshot{}, false, nil
	}

	snapshot := Snapshot{
		Version:          stored.Version,
		CachedLimit:      limit,
		Title:            stored.Title,
		TitleInitialized: stored.TitleInitialized,
		Messages:         make([]*schema.Message, 0, len(stored.Messages)),
	}
	for _, data := range stored.Messages {
		msg, err := decodeMessage([]byte(data))
		if err != nil {
			return Snapshot{}, false, err
		}
		snapshot.Messages = append(snapshot.Messages, msg)
	}
	return snapshot, true, nil
}

// GetOrCreateTitle 原子保存首个候选标题，并返回当前稳定标题。
func (m *Postgres) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string) (string, error) {
	if userId == "" || conversationId == "" {
		return "", fmt.Errorf("memory: user id or conversation id is empty")
	}
	if !m.available() {
		return "", fmt.Errorf("memory: postgres database is nil")
	}

	title, err := m.model.GetOrCreateTitle(ctx, conversation_memory.GetOrCreateTitleReq{
		UserId:         userId,
		ConversationId: conversationId,
		Candidate:      candidate,
		Now:            m.now(),
	})
	if err != nil {
		return "", fmt.Errorf("memory: get or create postgres title: %w", err)
	}
	return title, nil
}

// Clear 删除会话元数据，消息由复合外键级联删除。
func (m *Postgres) Clear(ctx context.Context, userId, conversationId string) error {
	if !m.available() {
		return fmt.Errorf("memory: postgres database is nil")
	}
	if err := m.model.Clear(ctx, userId, conversationId); err != nil {
		return fmt.Errorf("memory: clear postgres conversation: %w", err)
	}
	return nil
}

func (m *Postgres) available() bool {
	return m != nil && m.db != nil && m.model != nil
}
