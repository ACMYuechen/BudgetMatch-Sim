package mall_order_event_inbox

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"budgetmatch-sim/infra/uuid"
)

const (
	StatusPending = iota
	StatusProcessing
	StatusDone
)

type ClaimResult int

const (
	Claimed ClaimResult = iota
	AlreadyProcessed
	AlreadyProcessing
)

type MallOrderEventInbox struct {
	Id           string    `json:"id" gorm:"type:text;primaryKey;comment:Inbox记录ID"`
	ConsumerName string    `json:"consumer_name" gorm:"type:text;not null;uniqueIndex:uk_mall_order_event_inbox_consumer_dedup,priority:1;comment:消费者名称"`
	DedupKey     string    `json:"dedup_key" gorm:"type:text;not null;uniqueIndex:uk_mall_order_event_inbox_consumer_dedup,priority:2;comment:事件去重键"`
	EventType    string    `json:"event_type" gorm:"type:text;not null;index;comment:事件类型"`
	Status       int       `json:"status" gorm:"type:smallint;not null;default:0;index;comment:状态，0:待处理 1:处理中 2:已完成"`
	Attempts     int       `json:"attempts" gorm:"type:int;not null;default:0;comment:处理次数"`
	LockedUntil  time.Time `json:"locked_until" gorm:"type:timestamptz;not null;index;comment:处理租约到期时间"`
	LastError    string    `json:"last_error" gorm:"type:text;not null;default:'';comment:最后一次处理错误"`
	ProcessedAt  int64     `json:"processed_at" gorm:"type:bigint;not null;default:0;comment:处理完成Unix时间"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
}

type MallOrderEventInboxModel interface {
	CreateTable() error
	Claim(ctx context.Context, consumerName, dedupKey, eventType string, now, lockedUntil time.Time) (ClaimResult, error)
	MarkDone(ctx context.Context, consumerName, dedupKey string, now time.Time) (bool, error)
	MarkRetry(ctx context.Context, consumerName, dedupKey, lastError string, now time.Time) (bool, error)
}

type customMallOrderEventInboxModel struct {
	conn *gorm.DB
}

func NewMallOrderEventInboxModel(conn *gorm.DB) MallOrderEventInboxModel {
	return &customMallOrderEventInboxModel{conn: conn}
}

func (MallOrderEventInbox) TableName() string {
	return "mall_order_event_inbox"
}

func (m *customMallOrderEventInboxModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrderEventInbox{})
}

func (m *customMallOrderEventInboxModel) Claim(ctx context.Context, consumerName, dedupKey, eventType string, now, lockedUntil time.Time) (ClaimResult, error) {
	result := AlreadyProcessing
	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := &MallOrderEventInbox{
			Id:           uuid.NewPrefixedShortUUID("ibx-"),
			ConsumerName: consumerName,
			DedupKey:     dedupKey,
			EventType:    eventType,
			Status:       StatusProcessing,
			Attempts:     1,
			LockedUntil:  lockedUntil,
		}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected > 0 {
			result = Claimed
			return nil
		}

		existing := &MallOrderEventInbox{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("consumer_name = ? AND dedup_key = ?", consumerName, dedupKey).
			First(existing).Error; err != nil {
			return err
		}
		if existing.Status == StatusDone {
			result = AlreadyProcessed
			return nil
		}
		if existing.Status == StatusProcessing && existing.LockedUntil.After(now) {
			result = AlreadyProcessing
			return nil
		}

		updated := tx.Model(&MallOrderEventInbox{}).
			Where("id = ? AND status <> ?", existing.Id, StatusDone).
			Updates(map[string]any{
				"status":       StatusProcessing,
				"attempts":     gorm.Expr("attempts + 1"),
				"locked_until": lockedUntil,
				"last_error":   "",
				"updated_at":   now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return errors.New("inbox claim affected no rows")
		}
		result = Claimed
		return nil
	})
	return result, err
}

func (m *customMallOrderEventInboxModel) MarkDone(ctx context.Context, consumerName, dedupKey string, now time.Time) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderEventInbox{}).
		Where("consumer_name = ? AND dedup_key = ? AND status = ?", consumerName, dedupKey, StatusProcessing).
		Updates(map[string]any{
			"status":       StatusDone,
			"locked_until": now,
			"last_error":   "",
			"processed_at": now.Unix(),
			"updated_at":   now,
		})
	return result.RowsAffected > 0, result.Error
}

func (m *customMallOrderEventInboxModel) MarkRetry(ctx context.Context, consumerName, dedupKey, lastError string, now time.Time) (bool, error) {
	result := m.conn.WithContext(ctx).Model(&MallOrderEventInbox{}).
		Where("consumer_name = ? AND dedup_key = ? AND status = ?", consumerName, dedupKey, StatusProcessing).
		Updates(map[string]any{
			"status":       StatusPending,
			"locked_until": now,
			"last_error":   lastError,
			"updated_at":   now,
		})
	return result.RowsAffected > 0, result.Error
}
