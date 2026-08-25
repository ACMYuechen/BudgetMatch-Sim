// Package conversation_memory 提供 Agent 会话与轮次的 GORM model。
package conversation_memory

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// JSONDocument 为 PostgreSQL jsonb 字段提供明确的 Scanner/Valuer 语义。
// 直接使用 string 会让驱动把参数声明为 text，可能在真实 PostgreSQL 写入时缺少隐式转换。
type JSONDocument []byte

// Value 实现 driver.Valuer，并在写入前拒绝非法 JSON。
func (document JSONDocument) Value() (driver.Value, error) {
	if len(document) == 0 {
		return nil, nil
	}
	if !json.Valid(document) {
		return nil, fmt.Errorf("conversation memory: invalid JSON document")
	}
	return string(document), nil
}

// Scan 实现 sql.Scanner，兼容 PostgreSQL 驱动返回的 string 和 []byte。
func (document *JSONDocument) Scan(value any) error {
	if value == nil {
		*document = nil
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("conversation memory: cannot scan JSON from %T", value)
	}
	if !json.Valid(data) {
		return fmt.Errorf("conversation memory: database returned invalid JSON")
	}
	*document = append((*document)[:0], data...)
	return nil
}

// GormDataType 声明 JSONDocument 在 GORM 内部的通用逻辑类型。
func (JSONDocument) GormDataType() string { return "json" }

// GormDBDataType 指定 PostgreSQL DDL 使用原生 JSONB 字段。
func (JSONDocument) GormDBDataType(*gorm.DB, *schema.Field) string { return "JSONB" }

// GormValue 显式 CAST 参数为 JSONB，避免驱动按 text 绑定后发生类型不匹配。
func (document JSONDocument) GormValue(context.Context, *gorm.DB) clause.Expr {
	if len(document) == 0 {
		return gorm.Expr("NULL")
	}
	return gorm.Expr("CAST(? AS JSONB)", string(document))
}

const (
	conversationTable     = "agent_conversations"
	turnTable             = "agent_conversation_turns"
	conversationRecentIdx = "idx_agent_conversations_user_updated"
	turnSequenceIdx       = "idx_agent_conversation_turns_sequence"
	turnConversationFK    = "fk_agent_conversation_turns_conversation"
	TurnStatusCompleted   = "completed"
)

// AgentConversation 保存可恢复的会话元数据和最新结构化状态。
type AgentConversation struct {
	UserId         string       `json:"user_id" gorm:"column:user_id;type:text;primaryKey;index:idx_agent_conversations_user_updated,priority:1"`
	ConversationId string       `json:"conversation_id" gorm:"column:conversation_id;type:text;primaryKey"`
	Title          string       `json:"title" gorm:"column:title;type:text;not null"`
	State          JSONDocument `json:"state" gorm:"column:state;type:jsonb;not null"`
	Version        int64        `json:"version" gorm:"column:version;type:bigint;not null;default:0"`
	TurnCount      int64        `json:"turn_count" gorm:"column:turn_count;type:bigint;not null;default:0"`
	CreatedAt      time.Time    `json:"created_at" gorm:"column:created_at;type:timestamptz;not null"`
	UpdatedAt      time.Time    `json:"updated_at" gorm:"column:updated_at;type:timestamptz;not null;index:idx_agent_conversations_user_updated,priority:2,sort:desc"`

	// Turns 在 GORM 中声明父子关系，确保复合外键创建在轮次表并指向会话表。
	Turns []AgentConversationTurn `json:"-" gorm:"foreignKey:UserId,ConversationId;references:UserId,ConversationId;constraint:fk_agent_conversation_turns_conversation,OnDelete:CASCADE"`
}

// TableName 返回会话元数据表名。
func (AgentConversation) TableName() string { return conversationTable }

// AgentConversationTurn 保存一轮不可变请求和完整结果。
type AgentConversationTurn struct {
	UserId         string       `json:"user_id" gorm:"column:user_id;type:text;primaryKey;index:idx_agent_conversation_turns_sequence,priority:1,unique"`
	ConversationId string       `json:"conversation_id" gorm:"column:conversation_id;type:text;primaryKey;index:idx_agent_conversation_turns_sequence,priority:2,unique"`
	TurnId         string       `json:"turn_id" gorm:"column:turn_id;type:text;primaryKey"`
	Sequence       int64        `json:"sequence" gorm:"column:sequence;type:bigint;not null;index:idx_agent_conversation_turns_sequence,priority:3,unique"`
	Status         string       `json:"status" gorm:"column:status;type:text;not null"`
	Query          string       `json:"query" gorm:"column:query;type:text;not null"`
	BudgetCents    int64        `json:"budget_cents" gorm:"column:budget_cents;type:bigint;not null"`
	MaxItems       int32        `json:"max_items" gorm:"column:max_items;type:integer;not null"`
	Intent         JSONDocument `json:"intent" gorm:"column:intent;type:jsonb;not null"`
	Result         JSONDocument `json:"result" gorm:"column:result;type:jsonb;not null"`
	Summary        string       `json:"summary" gorm:"column:summary;type:text;not null"`
	CreatedAt      time.Time    `json:"created_at" gorm:"column:created_at;type:timestamptz;not null"`
	CompletedAt    *time.Time   `json:"completed_at" gorm:"column:completed_at;type:timestamptz"`
}

// TableName 返回完整推荐轮次表名。
func (AgentConversationTurn) TableName() string { return turnTable }

// SaveTurnReq 描述 model 层一次原子会话更新所需的数据。
type SaveTurnReq struct {
	UserId         string
	ConversationId string
	TurnId         string
	Title          string
	State          string
	Query          string
	BudgetCents    int64
	MaxItems       int32
	Intent         string
	Result         string
	Summary        string
	Now            time.Time
}

// ConversationMemoryModel 定义 PostgreSQL 会话与轮次的 GORM 数据访问能力。
type ConversationMemoryModel interface {
	CreateTable() error
	CheckSchema() error
	Version(ctx context.Context, userId, conversationId string) (int64, bool, error)
	GetConversation(ctx context.Context, userId, conversationId string) (AgentConversation, bool, error)
	FindTurn(ctx context.Context, userId, conversationId, turnId string) (AgentConversationTurn, bool, error)
	GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string, now time.Time) (string, error)
	SaveTurn(ctx context.Context, req SaveTurnReq) (AgentConversation, AgentConversationTurn, error)
	ListConversations(ctx context.Context, userId string, offset, limit int) ([]AgentConversation, int64, error)
	ListTurns(ctx context.Context, userId, conversationId string, offset, limit int) (AgentConversation, []AgentConversationTurn, int64, bool, error)
	DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error)
}

// defaultConversationMemoryModel 是 ConversationMemoryModel 的默认 GORM 实现。
type defaultConversationMemoryModel struct{ conn *gorm.DB }

var _ ConversationMemoryModel = (*defaultConversationMemoryModel)(nil)

// NewConversationMemoryModel 创建绑定指定 GORM 连接的数据访问对象。
func NewConversationMemoryModel(conn *gorm.DB) ConversationMemoryModel {
	return &defaultConversationMemoryModel{conn: conn}
}

// CreateTable 使用 AutoMigrate 创建会话、轮次及声明的索引和外键。
func (m *defaultConversationMemoryModel) CreateTable() error {
	return m.conn.AutoMigrate(&AgentConversation{}, &AgentConversationTurn{})
}

// CheckSchema 在启动时验证长期记忆所需的数据库结构，防止带缺失 schema 运行。
func (m *defaultConversationMemoryModel) CheckSchema() error {
	migrator := m.conn.Migrator()
	checks := []struct {
		ok   bool
		name string
	}{
		{migrator.HasTable(&AgentConversation{}), "table " + conversationTable},
		{migrator.HasColumn(&AgentConversation{}, "state"), "column " + conversationTable + ".state"},
		{migrator.HasColumn(&AgentConversation{}, "turn_count"), "column " + conversationTable + ".turn_count"},
		{migrator.HasIndex(&AgentConversation{}, conversationRecentIdx), "index " + conversationRecentIdx},
		{migrator.HasTable(&AgentConversationTurn{}), "table " + turnTable},
		{migrator.HasColumn(&AgentConversationTurn{}, "result"), "column " + turnTable + ".result"},
		{migrator.HasIndex(&AgentConversationTurn{}, turnSequenceIdx), "index " + turnSequenceIdx},
		{migrator.HasConstraint(&AgentConversationTurn{}, turnConversationFK), "constraint " + turnConversationFK},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("conversation memory schema is missing %s", check.name)
		}
	}
	return nil
}

// Version 返回指定用户会话的当前缓存一致性版本。
func (m *defaultConversationMemoryModel) Version(ctx context.Context, userId, conversationId string) (int64, bool, error) {
	conversation, exists, err := m.GetConversation(ctx, userId, conversationId)
	return conversation.Version, exists, err
}

// GetConversation 使用 user_id 与 conversation_id 复合键隔离用户数据。
func (m *defaultConversationMemoryModel) GetConversation(ctx context.Context, userId, conversationId string) (AgentConversation, bool, error) {
	var conversation AgentConversation
	err := m.conn.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Take(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentConversation{}, false, nil
	}
	return conversation, err == nil, err
}

// FindTurn 按复合主键查询轮次，用于判断 turn_id 是否已经完成。
func (m *defaultConversationMemoryModel) FindTurn(ctx context.Context, userId, conversationId, turnId string) (AgentConversationTurn, bool, error) {
	var turn AgentConversationTurn
	err := m.conn.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ? AND turn_id = ?", userId, conversationId, turnId).
		Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentConversationTurn{}, false, nil
	}
	return turn, err == nil, err
}

// GetOrCreateTitle 通过 ON CONFLICT DO NOTHING 保留并发写入时的首个标题。
func (m *defaultConversationMemoryModel) GetOrCreateTitle(ctx context.Context, userId, conversationId, candidate string, now time.Time) (string, error) {
	conversation := AgentConversation{
		UserId: userId, ConversationId: conversationId, Title: candidate,
		State: JSONDocument(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := m.conn.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoNothing: true,
	}).Create(&conversation).Error; err != nil {
		return "", err
	}
	stored, _, err := m.GetConversation(ctx, userId, conversationId)
	return stored.Title, err
}

// SaveTurn 在一个事务内插入幂等轮次并更新会话最新状态。
// 调用方已持有跨实例会话锁；事务内行锁负责约束数据库内的顺序更新。
func (m *defaultConversationMemoryModel) SaveTurn(ctx context.Context, req SaveTurnReq) (AgentConversation, AgentConversationTurn, error) {
	var savedConversation AgentConversation
	var savedTurn AgentConversationTurn
	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing AgentConversationTurn
		err := tx.Where("user_id = ? AND conversation_id = ? AND turn_id = ?", req.UserId, req.ConversationId, req.TurnId).
			Take(&existing).Error
		if err == nil {
			savedTurn = existing
			return tx.Where("user_id = ? AND conversation_id = ?", req.UserId, req.ConversationId).
				Take(&savedConversation).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var conversation AgentConversation
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND conversation_id = ?", req.UserId, req.ConversationId).
			Take(&conversation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			conversation = AgentConversation{
				UserId: req.UserId, ConversationId: req.ConversationId, Title: req.Title,
				State: JSONDocument(req.State), Version: 0, TurnCount: 0, CreatedAt: req.Now, UpdatedAt: req.Now,
			}
			if err = tx.Create(&conversation).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		sequence := conversation.TurnCount + 1
		completedAt := req.Now
		turn := AgentConversationTurn{
			UserId: req.UserId, ConversationId: req.ConversationId, TurnId: req.TurnId,
			Sequence: sequence, Status: TurnStatusCompleted, Query: req.Query,
			BudgetCents: req.BudgetCents, MaxItems: req.MaxItems, Intent: JSONDocument(req.Intent),
			Result: JSONDocument(req.Result), Summary: req.Summary, CreatedAt: req.Now, CompletedAt: &completedAt,
		}
		if err = tx.Create(&turn).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"state": JSONDocument(req.State), "turn_count": sequence, "version": conversation.Version + 1, "updated_at": req.Now,
		}
		if conversation.Title == "" {
			updates["title"] = req.Title
			conversation.Title = req.Title
		}
		if err = tx.Model(&AgentConversation{}).
			Where("user_id = ? AND conversation_id = ?", req.UserId, req.ConversationId).
			Updates(updates).Error; err != nil {
			return err
		}
		conversation.State = JSONDocument(req.State)
		conversation.TurnCount = sequence
		conversation.Version++
		conversation.UpdatedAt = req.Now
		savedConversation, savedTurn = conversation, turn
		return nil
	})
	return savedConversation, savedTurn, err
}

// ListConversations 按更新时间倒序分页查询指定用户的会话及总数。
func (m *defaultConversationMemoryModel) ListConversations(ctx context.Context, userId string, offset, limit int) ([]AgentConversation, int64, error) {
	query := m.conn.WithContext(ctx).Model(&AgentConversation{}).Where("user_id = ?", userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var conversations []AgentConversation
	err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&conversations).Error
	return conversations, total, err
}

// ListTurns 按 sequence 正序分页查询轮次，并区分会话不存在和空页。
func (m *defaultConversationMemoryModel) ListTurns(ctx context.Context, userId, conversationId string, offset, limit int) (AgentConversation, []AgentConversationTurn, int64, bool, error) {
	conversation, exists, err := m.GetConversation(ctx, userId, conversationId)
	if err != nil || !exists {
		return AgentConversation{}, nil, 0, exists, err
	}
	query := m.conn.WithContext(ctx).Model(&AgentConversationTurn{}).
		Where("user_id = ? AND conversation_id = ? AND status = ?", userId, conversationId, TurnStatusCompleted)
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return AgentConversation{}, nil, 0, true, err
	}
	var turns []AgentConversationTurn
	err = query.Order("sequence ASC").Offset(offset).Limit(limit).Find(&turns).Error
	return conversation, turns, total, true, err
}

// DeleteConversation 删除会话元数据；数据库外键负责级联删除所有轮次。
func (m *defaultConversationMemoryModel) DeleteConversation(ctx context.Context, userId, conversationId string) (bool, error) {
	result := m.conn.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Delete(&AgentConversation{})
	return result.RowsAffected > 0, result.Error
}
