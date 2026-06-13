// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_order

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	seckillOrdersModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*SeckillOrders) error
		InsertOne(ctx context.Context, data *SeckillOrders) error
		List(ctx context.Context, req SeckillOrdersListReq) ([]SeckillOrders, int64, error)
		FindOne(ctx context.Context, id string) (*SeckillOrders, error)
		Update(ctx context.Context, data *SeckillOrders) error
		Delete(ctx context.Context, id string) error
	}

	defaultSeckillOrdersModel struct {
		conn  *gorm.DB
		table string
	}

	SeckillOrders struct {
		Id          string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:订单ID"`
		ActivityId  string         `json:"activity_id" gorm:"type:varchar(36);not null;index;uniqueIndex:uk_activity_sku_user;comment:活动ID"`
		SkuId       string         `json:"sku_id" gorm:"type:varchar(36);not null;index;uniqueIndex:uk_activity_sku_user;comment:SKU ID"`
		UserId      string         `json:"user_id" gorm:"type:varchar(36);not null;index;uniqueIndex:uk_activity_sku_user;comment:用户ID"`
		Quantity    int64          `json:"quantity" gorm:"type:int;default:1;comment:数量"`
		TotalAmount int64          `json:"total_amount" gorm:"type:bigint;not null;comment:总金额，单位：分"`
		Status      int64          `json:"status" gorm:"type:smallint;default:0;comment:状态，0:排队中 1:成功 2:失败 3:已支付 4:已关闭"`
		Snapshot    string         `json:"snapshot" gorm:"type:jsonb;comment:订单快照"`
		CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt   gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	SeckillOrdersListReq struct {
		Page int `json:"page"`
		Size int `json:"size"`
	}
)

func newSeckillOrdersModel(conn *gorm.DB) *defaultSeckillOrdersModel {
	return &defaultSeckillOrdersModel{
		conn:  conn,
		table: `"public"."seckill_orders"`,
	}
}

func (m *defaultSeckillOrdersModel) CreateTable() error {
	return m.conn.AutoMigrate(&SeckillOrders{})
}

func (m *defaultSeckillOrdersModel) Insert(ctx context.Context, data []*SeckillOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillOrdersModel) InsertOne(ctx context.Context, data *SeckillOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillOrdersModel) FindOne(ctx context.Context, id string) (*SeckillOrders, error) {
	model := &SeckillOrders{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultSeckillOrdersModel) Update(ctx context.Context, data *SeckillOrders) error {
	return m.conn.WithContext(ctx).Model(&SeckillOrders{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultSeckillOrdersModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&SeckillOrders{}).Error
}

func (m *defaultSeckillOrdersModel) List(ctx context.Context, req SeckillOrdersListReq) ([]SeckillOrders, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]SeckillOrders, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillOrders{})
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

func (m *defaultSeckillOrdersModel) tableName() string {
	return m.table
}
