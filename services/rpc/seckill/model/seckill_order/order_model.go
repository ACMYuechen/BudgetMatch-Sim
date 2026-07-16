// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_order

import (
	"budgetmatch-sim/infra/uuid"
	"context"
	"errors"

	"gorm.io/gorm"
)

type (
	SeckillOrdersModel interface {
		seckillOrdersModel
		FindByActivityAndSkuAndUser(ctx context.Context, activityId, skuId, userId string) (*SeckillOrders, error)
		ListByUser(ctx context.Context, userId string, page, size int) ([]SeckillOrders, int64, error)
		ListByActivity(ctx context.Context, activityId string, page, size int) ([]SeckillOrders, int64, error)
	}

	customSeckillOrdersModel struct {
		*defaultSeckillOrdersModel
	}
)

func NewSeckillOrdersModel(conn *gorm.DB) SeckillOrdersModel {
	return &customSeckillOrdersModel{
		defaultSeckillOrdersModel: newSeckillOrdersModel(conn),
	}
}

func NewSeckillOrderId() string {
	return uuid.NewPrefixedShortUUID("sord-")
}

func (m *customSeckillOrdersModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&SeckillOrders{}) {
		return m.conn.Migrator().CreateTable(&SeckillOrders{})
	}
	return nil
}

func (m *customSeckillOrdersModel) FindByActivityAndSkuAndUser(ctx context.Context, activityId, skuId, userId string) (*SeckillOrders, error) {
	model := &SeckillOrders{}
	err := m.conn.WithContext(ctx).Where("activity_id = ? AND sku_id = ? AND user_id = ?", activityId, skuId, userId).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *customSeckillOrdersModel) ListByUser(ctx context.Context, userId string, page, size int) ([]SeckillOrders, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillOrders, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillOrders{}).Where("user_id = ?", userId)

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("created_at DESC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *customSeckillOrdersModel) ListByActivity(ctx context.Context, activityId string, page, size int) ([]SeckillOrders, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillOrders, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillOrders{}).Where("activity_id = ?", activityId)

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("created_at DESC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}
