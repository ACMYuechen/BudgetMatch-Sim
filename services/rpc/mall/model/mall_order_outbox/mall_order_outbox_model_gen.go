// Code scaffolded by goctl. Only init once, Safe to edit.
// gorm

package mall_order_outbox

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	mallOrderOutboxModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*MallOrderOutbox) error
		InsertOne(ctx context.Context, data *MallOrderOutbox) error
		FindOne(ctx context.Context, id string) (*MallOrderOutbox, error)
		List(ctx context.Context, req MallOrderOutboxListReq) ([]MallOrderOutbox, int64, error)
		Update(ctx context.Context, data *MallOrderOutbox) error
		Delete(ctx context.Context, id string) error
	}

	defaultMallOrderOutboxModel struct {
		conn  *gorm.DB
		table string
	}

	MallOrderOutbox struct {
		Id            string `json:"id" gorm:"type:text;primaryKey;comment:Outbox事件ID"`
		AggregateType string `json:"aggregate_type" gorm:"type:text;not null;comment:聚合类型"`
		AggregateId   string `json:"aggregate_id" gorm:"type:text;not null;index;comment:聚合ID"`
		EventType     string `json:"event_type" gorm:"type:text;not null;comment:事件类型"`
		DedupKey      string `json:"dedup_key" gorm:"type:text;not null;uniqueIndex;comment:事件去重键"`
		Topic         string `json:"topic" gorm:"type:text;not null;comment:MQ主题"`
		Tag           string `json:"tag" gorm:"type:text;not null;comment:MQ标签"`
		MessageKey    string `json:"message_key" gorm:"type:text;not null;comment:MQ消息键"`
		Payload       string `json:"payload" gorm:"type:jsonb;not null;comment:消息体"`

		Status      int       `json:"status" gorm:"type:smallint;not null;default:0;index:idx_mall_order_outbox_dispatch,priority:1;comment:状态，0:待发送 1:发送中 2:已发送 3:死信"`
		Attempts    int       `json:"attempts" gorm:"type:int;not null;default:0;comment:已领取次数"`
		MaxAttempts int       `json:"max_attempts" gorm:"type:int;not null;default:10;comment:最大领取次数"`
		NextRetryAt time.Time `json:"next_retry_at" gorm:"type:timestamptz;not null;index:idx_mall_order_outbox_dispatch,priority:2;comment:下次重试时间"`
		LockedUntil time.Time `json:"locked_until" gorm:"type:timestamptz;not null;index;comment:处理租约到期时间"`
		LastError   string    `json:"last_error" gorm:"type:text;not null;default:'';comment:最后一次发送错误"`
		PublishedAt int64     `json:"published_at" gorm:"type:bigint;not null;default:0;comment:发送成功Unix时间"`

		CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
	}
)

func newMallOrderOutboxModel(conn *gorm.DB) *defaultMallOrderOutboxModel {
	return &defaultMallOrderOutboxModel{
		conn:  conn,
		table: `"public"."mall_order_outbox"`,
	}
}

func (m *defaultMallOrderOutboxModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&MallOrderOutbox{}).Error
}

func (m *defaultMallOrderOutboxModel) FindOne(ctx context.Context, id string) (*MallOrderOutbox, error) {
	model := &MallOrderOutbox{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

type MallOrderOutboxListReq struct {
	Page int `json:"page"` // Page number
	Size int `json:"size"` // Number of items per page
}

func (m *defaultMallOrderOutboxModel) List(ctx context.Context, req MallOrderOutboxListReq) ([]MallOrderOutbox, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]MallOrderOutbox, 0)
	session := m.conn.WithContext(ctx).Model(&MallOrderOutbox{})
	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, err
	}

	offset := (req.Page - 1) * req.Size
	err = session.Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *defaultMallOrderOutboxModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrderOutbox{})
}

func (m *defaultMallOrderOutboxModel) Insert(ctx context.Context, data []*MallOrderOutbox) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrderOutboxModel) InsertOne(ctx context.Context, data *MallOrderOutbox) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrderOutboxModel) Update(ctx context.Context, data *MallOrderOutbox) error {
	return m.conn.WithContext(ctx).Model(&MallOrderOutbox{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultMallOrderOutboxModel) tableName() string {
	return m.table
}
