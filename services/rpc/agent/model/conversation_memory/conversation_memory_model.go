// Package conversation_memory 提供 Agent 会话长期记忆的 GORM model。
package conversation_memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	conversationTable     = "agent_conversations"
	messageTable          = "agent_conversation_messages"
	recentMessageIndex    = "idx_agent_conversation_messages_recent"
	messageConversationFK = "fk_agent_conversation_messages_conversation"
)

// AgentConversation 保存按用户隔离的会话元数据、稳定标题和缓存版本。
type AgentConversation struct {
	UserId           string    `json:"user_id" gorm:"column:user_id;type:text;primaryKey"`
	ConversationId   string    `json:"conversation_id" gorm:"column:conversation_id;type:text;primaryKey"`
	Title            string    `json:"title" gorm:"column:title;type:text;not null;default:''"`
	TitleInitialized bool      `json:"title_initialized" gorm:"column:title_initialized;not null;default:false"`
	Version          int64     `json:"version" gorm:"column:version;type:bigint;not null;default:0"`
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at;type:timestamptz;not null;autoCreateTime"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"column:updated_at;type:timestamptz;not null;autoUpdateTime"`
}

// TableName 返回会话元数据表名。
func (AgentConversation) TableName() string {
	return conversationTable
}

// AgentConversationMessage 顺序保存会话的完整消息 JSON。
type AgentConversationMessage struct {
	Id             int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;index:idx_agent_conversation_messages_recent,priority:3,sort:desc"`
	UserId         string    `json:"user_id" gorm:"column:user_id;type:text;not null;index:idx_agent_conversation_messages_recent,priority:1"`
	ConversationId string    `json:"conversation_id" gorm:"column:conversation_id;type:text;not null;index:idx_agent_conversation_messages_recent,priority:2"`
	Message        string    `json:"message" gorm:"column:message;type:jsonb;not null"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at;type:timestamptz;not null;autoCreateTime"`

	Conversation *AgentConversation `json:"-" gorm:"foreignKey:UserId,ConversationId;references:UserId,ConversationId;constraint:fk_agent_conversation_messages_conversation,OnDelete:CASCADE"`
}

// TableName 返回会话消息表名。
func (AgentConversationMessage) TableName() string {
	return messageTable
}

// AppendReq 是一次会话消息追加事务的参数。
type AppendReq struct {
	UserId         string
	ConversationId string
	Messages       []string
	Now            time.Time
}

// GetOrCreateTitleReq 是稳定标题原子初始化的参数。
type GetOrCreateTitleReq struct {
	UserId         string
	ConversationId string
	Candidate      string
	Now            time.Time
}

// Snapshot 是持久层一次读取返回的会话元数据与消息 JSON。
type Snapshot struct {
	Version          int64
	Title            string
	TitleInitialized bool
	Messages         []string
}

// ConversationMemoryModel 定义会话长期记忆所需的数据库操作。
// 会话表使用复合主键，项目当前的单 id CRUD 模板不适用，因此这些方法按同样分层约定手写 GORM 实现。
type ConversationMemoryModel interface {
	CreateTable() error
	CheckSchema() error
	Append(ctx context.Context, req AppendReq) error
	Version(ctx context.Context, userId, conversationId string) (int64, bool, error)
	LoadSnapshot(ctx context.Context, userId, conversationId string, limit int) (Snapshot, bool, error)
	GetOrCreateTitle(ctx context.Context, req GetOrCreateTitleReq) (string, error)
	Clear(ctx context.Context, userId, conversationId string) error
}

type defaultConversationMemoryModel struct {
	conn *gorm.DB
}

var _ ConversationMemoryModel = (*defaultConversationMemoryModel)(nil)

// NewConversationMemoryModel 创建会话长期记忆 model。
func NewConversationMemoryModel(conn *gorm.DB) ConversationMemoryModel {
	return &defaultConversationMemoryModel{conn: conn}
}

// CreateTable 使用 GORM 幂等迁移会话表、消息表、复合外键和最近消息索引。
func (m *defaultConversationMemoryModel) CreateTable() error {
	return m.conn.AutoMigrate(&AgentConversation{}, &AgentConversationMessage{})
}

// CheckSchema 校验关闭 AutoMigrate 时运行所需的表、列、索引与级联外键。
func (m *defaultConversationMemoryModel) CheckSchema() error {
	migrator := m.conn.Migrator()
	checks := []struct {
		ok   bool
		name string
	}{
		{migrator.HasTable(&AgentConversation{}), "table " + conversationTable},
		{migrator.HasColumn(&AgentConversation{}, "title_initialized"), "column " + conversationTable + ".title_initialized"},
		{migrator.HasColumn(&AgentConversation{}, "version"), "column " + conversationTable + ".version"},
		{migrator.HasTable(&AgentConversationMessage{}), "table " + messageTable},
		{migrator.HasColumn(&AgentConversationMessage{}, "message"), "column " + messageTable + ".message"},
		{migrator.HasIndex(&AgentConversationMessage{}, recentMessageIndex), "index " + recentMessageIndex},
		{migrator.HasConstraint(&AgentConversationMessage{}, messageConversationFK), "constraint " + messageConversationFK},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("conversation memory schema is missing %s", check.name)
		}
	}
	return nil
}

// Append 在一个事务中创建或刷新会话版本，并批量追加消息。
func (m *defaultConversationMemoryModel) Append(ctx context.Context, req AppendReq) error {
	if len(req.Messages) == 0 {
		return nil
	}
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation := &AgentConversation{
			UserId:         req.UserId,
			ConversationId: req.ConversationId,
			Version:        1,
			CreatedAt:      req.Now,
			UpdatedAt:      req.Now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"version":    gorm.Expr(conversationTable + ".version + 1"),
				"updated_at": req.Now,
			}),
		}).Create(conversation).Error; err != nil {
			return err
		}

		messages := make([]AgentConversationMessage, len(req.Messages))
		for i, message := range req.Messages {
			messages[i] = AgentConversationMessage{
				UserId:         req.UserId,
				ConversationId: req.ConversationId,
				Message:        message,
				CreatedAt:      req.Now,
			}
		}
		return tx.Create(&messages).Error
	})
}

// Version 返回会话缓存版本；会话不存在时 exists=false。
func (m *defaultConversationMemoryModel) Version(ctx context.Context, userId, conversationId string) (int64, bool, error) {
	row := &AgentConversation{}
	err := m.conn.WithContext(ctx).
		Select("version").
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Take(row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.Version, true, nil
}

// LoadSnapshot 用一条 PostgreSQL MVCC 查询同时读取会话元数据和最近消息。
func (m *defaultConversationMemoryModel) LoadSnapshot(ctx context.Context, userId, conversationId string, limit int) (Snapshot, bool, error) {
	var rows []struct {
		Version          int64          `gorm:"column:version"`
		Title            string         `gorm:"column:title"`
		TitleInitialized bool           `gorm:"column:title_initialized"`
		Message          sql.NullString `gorm:"column:message"`
	}
	result := m.conn.WithContext(ctx).Raw(`SELECT
			c.version,
			c.title,
			c.title_initialized,
			recent.message::text AS message
		FROM agent_conversations AS c
		LEFT JOIN LATERAL (
			SELECT id, message
			FROM agent_conversation_messages
			WHERE user_id = c.user_id AND conversation_id = c.conversation_id
			ORDER BY id DESC
			LIMIT ?
		) AS recent ON TRUE
		WHERE c.user_id = ? AND c.conversation_id = ?
		ORDER BY recent.id ASC`, limit, userId, conversationId).Scan(&rows)
	if result.Error != nil {
		return Snapshot{}, false, result.Error
	}
	if len(rows) == 0 {
		return Snapshot{}, false, nil
	}

	snapshot := Snapshot{
		Version:          rows[0].Version,
		Title:            rows[0].Title,
		TitleInitialized: rows[0].TitleInitialized,
		Messages:         make([]string, 0, len(rows)),
	}
	for _, row := range rows {
		if row.Message.Valid {
			snapshot.Messages = append(snapshot.Messages, row.Message.String)
		}
	}
	return snapshot, true, nil
}

// GetOrCreateTitle 原子保存首个候选标题，并返回当前稳定标题。
func (m *defaultConversationMemoryModel) GetOrCreateTitle(ctx context.Context, req GetOrCreateTitleReq) (string, error) {
	conversation := &AgentConversation{
		UserId:           req.UserId,
		ConversationId:   req.ConversationId,
		Title:            req.Candidate,
		TitleInitialized: true,
		Version:          1,
		CreatedAt:        req.Now,
		UpdatedAt:        req.Now,
	}
	err := m.conn.WithContext(ctx).Clauses(
		clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"title": gorm.Expr(`CASE
					WHEN NOT agent_conversations.title_initialized THEN EXCLUDED.title
					ELSE agent_conversations.title
				END`),
				"title_initialized": true,
				"version": gorm.Expr(`CASE
					WHEN NOT agent_conversations.title_initialized THEN agent_conversations.version + 1
					ELSE agent_conversations.version
				END`),
				"updated_at": gorm.Expr(`CASE
					WHEN NOT agent_conversations.title_initialized THEN EXCLUDED.updated_at
					ELSE agent_conversations.updated_at
				END`),
			}),
		},
		clause.Returning{Columns: []clause.Column{{Name: "title"}}},
	).Create(conversation).Error
	return conversation.Title, err
}

// Clear 删除会话元数据，消息由数据库复合外键级联删除。
func (m *defaultConversationMemoryModel) Clear(ctx context.Context, userId, conversationId string) error {
	return m.conn.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Delete(&AgentConversation{}).Error
}
