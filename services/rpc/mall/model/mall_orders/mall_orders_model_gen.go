// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_orders

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	mallOrdersModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*MallOrders) error
		InsertOne(ctx context.Context, data *MallOrders) error
		List(ctx context.Context, req MallOrdersListReq) ([]MallOrders, int64, error)
		FindOne(ctx context.Context, id string) (*MallOrders, error)
		FindByIdempotencyKey(ctx context.Context, key string) (*MallOrders, error)
		Update(ctx context.Context, data *MallOrders) error
		Delete(ctx context.Context, id string) error
	}

	defaultMallOrdersModel struct {
		conn  *gorm.DB
		table string
	}

	MallOrders struct {
		Id             string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:订单ID"`
		UserId         string         `json:"user_id" gorm:"type:varchar(36);not null;index;comment:用户ID"`
		TotalAmount    int64          `json:"total_amount" gorm:"type:bigint;not null;comment:总金额，单位：分"`
		Status         int64          `json:"status" gorm:"type:smallint;default:0;comment:状态，0:pending 1:paid 2:shipped 3:completed 4:cancelled 5:refunding 6:refunded"`
		PayType        string         `json:"pay_type" gorm:"type:varchar(50);comment:支付方式"`
		PayTime        *time.Time     `json:"pay_time" gorm:"type:timestamptz;comment:支付时间"`
		Remark         string         `json:"remark" gorm:"type:varchar(500);comment:用户备注"`
		Snapshot       string         `json:"snapshot" gorm:"type:jsonb;comment:订单快照"`
		IdempotencyKey string         `json:"idempotency_key" gorm:"type:varchar(64);not null;uniqueIndex;comment:幂等键"`
		CreatedAt      time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt      time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt      gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	MallOrdersListReq struct {
		Page     int
		Size     int
		UserId   string
		Status   int64
	}
)

func newMallOrdersModel(conn *gorm.DB) *defaultMallOrdersModel {
	return &defaultMallOrdersModel{
		conn:  conn,
		table: `"public"."mall_orders"`,
	}
}

func (m *defaultMallOrdersModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrders{})
}

func (m *defaultMallOrdersModel) Insert(ctx context.Context, data []*MallOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrdersModel) InsertOne(ctx context.Context, data *MallOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrdersModel) FindOne(ctx context.Context, id string) (*MallOrders, error) {
	model := &MallOrders{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultMallOrdersModel) FindByIdempotencyKey(ctx context.Context, key string) (*MallOrders, error) {
	model := &MallOrders{}
	err := m.conn.WithContext(ctx).Where("idempotency_key = ?", key).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultMallOrdersModel) Update(ctx context.Context, data *MallOrders) error {
	return m.conn.WithContext(ctx).Model(&MallOrders{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultMallOrdersModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&MallOrders{}).Error
}

func (m *defaultMallOrdersModel) List(ctx context.Context, req MallOrdersListReq) ([]MallOrders, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]MallOrders, 0)
	session := m.conn.WithContext(ctx).Model(&MallOrders{})

	if req.UserId != "" {
		session = session.Where("user_id = ?", req.UserId)
	}
	if req.Status >= 0 {
		session = session.Where("status = ?", req.Status)
	}

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (req.Page - 1) * req.Size
	err = session.Order("created_at DESC").Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *defaultMallOrdersModel) tableName() string {
	return m.table
}
