// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_activity

import (
	"context"

	"gorm.io/gorm"
)

var _ SeckillActivitiesModel = (*customSeckillActivitiesModel)(nil)

type (
	SeckillActivitiesModel interface {
		seckillActivitiesModel
		ListByStatus(ctx context.Context, status int64, page, size int) ([]SeckillActivities, int64, error)
		ListByTimeRange(ctx context.Context, startTime, endTime string, page, size int) ([]SeckillActivities, int64, error)
	}

	customSeckillActivitiesModel struct {
		*defaultSeckillActivitiesModel
	}
)

func NewSeckillActivitiesModel(conn *gorm.DB) SeckillActivitiesModel {
	return &customSeckillActivitiesModel{
		defaultSeckillActivitiesModel: newSeckillActivitiesModel(conn),
	}
}

func (m *customSeckillActivitiesModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&SeckillActivities{}) {
		return m.conn.Migrator().CreateTable(&SeckillActivities{})
	}
	return nil
}

func (m *customSeckillActivitiesModel) ListByStatus(ctx context.Context, status int64, page, size int) ([]SeckillActivities, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillActivities, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillActivities{}).Where("status = ?", status)

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("start_time ASC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *customSeckillActivitiesModel) ListByTimeRange(ctx context.Context, startTime, endTime string, page, size int) ([]SeckillActivities, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	total := int64(0)
	list := make([]SeckillActivities, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillActivities{})

	if startTime != "" {
		session = session.Where("start_time >= ?", startTime)
	}
	if endTime != "" {
		session = session.Where("end_time <= ?", endTime)
	}

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (page - 1) * size
	err = session.Order("start_time ASC").Limit(size).Offset(offset).Find(&list).Error

	return list, total, err
}
