// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_sku

import (
	"budgetmatch-sim/infra/uuid"
	"context"

	"gorm.io/gorm"
)

type (
	SeckillSkusModel interface {
		seckillSkusModel
		ListByActivity(ctx context.Context, activityId string, page, size int) ([]SeckillSkus, int64, error)
		ListByActivityAndStatus(ctx context.Context, activityId string, status int64, page, size int) ([]SeckillSkus, int64, error)
	}

	customSeckillSkusModel struct {
		*defaultSeckillSkusModel
	}
)

func NewSeckillSkusModel(conn *gorm.DB) SeckillSkusModel {
	return &customSeckillSkusModel{
		defaultSeckillSkusModel: newSeckillSkusModel(conn),
	}
}

func NewSeckillSkuId() string {
	return uuid.NewPrefixedShortUUID("ssku-")
}

func (m *customSeckillSkusModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&SeckillSkus{}) {
		return m.conn.Migrator().CreateTable(&SeckillSkus{})
	}
	return nil
}

func (m *customSeckillSkusModel) ListByActivity(ctx context.Context, activityId string, page, size int) ([]SeckillSkus, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillSkus, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillSkus{}).Where("activity_id = ?", activityId)

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("sort ASC, created_at DESC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *customSeckillSkusModel) ListByActivityAndStatus(ctx context.Context, activityId string, status int64, page, size int) ([]SeckillSkus, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillSkus, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillSkus{}).Where("activity_id = ? AND status = ?", activityId, status)

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("sort ASC, created_at DESC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}
