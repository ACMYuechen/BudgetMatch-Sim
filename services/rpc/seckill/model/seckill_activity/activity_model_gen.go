// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_activity

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	seckillActivitiesModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*SeckillActivities) error
		InsertOne(ctx context.Context, data *SeckillActivities) error
		List(ctx context.Context, req SeckillActivitiesListReq) ([]SeckillActivities, int64, error)
		FindOne(ctx context.Context, id string) (*SeckillActivities, error)
		Update(ctx context.Context, data *SeckillActivities) error
		Delete(ctx context.Context, id string) error
	}

	defaultSeckillActivitiesModel struct {
		conn  *gorm.DB
		table string
	}

	SeckillActivities struct {
		Id          string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:活动ID"`
		Title       string         `json:"title" gorm:"type:varchar(255);not null;comment:活动标题"`
		Description string         `json:"description" gorm:"type:text;comment:活动描述"`
		BannerUrl   string         `json:"banner_url" gorm:"type:varchar(500);comment:Banner图片URL"`
		StartTime   time.Time      `json:"start_time" gorm:"type:timestamptz;not null;comment:开始时间"`
		EndTime     time.Time      `json:"end_time" gorm:"type:timestamptz;not null;comment:结束时间"`
		Status      int64          `json:"status" gorm:"type:smallint;default:0;comment:状态，0:下线 1:上线 2:预热中"`
		CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt   gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	SeckillActivitiesListReq struct {
		Page int `json:"page"`
		Size int `json:"size"`
	}
)

func newSeckillActivitiesModel(conn *gorm.DB) *defaultSeckillActivitiesModel {
	return &defaultSeckillActivitiesModel{
		conn:  conn,
		table: `"public"."seckill_activities"`,
	}
}

func (m *defaultSeckillActivitiesModel) CreateTable() error {
	return m.conn.AutoMigrate(&SeckillActivities{})
}

func (m *defaultSeckillActivitiesModel) Insert(ctx context.Context, data []*SeckillActivities) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillActivitiesModel) InsertOne(ctx context.Context, data *SeckillActivities) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillActivitiesModel) FindOne(ctx context.Context, id string) (*SeckillActivities, error) {
	model := &SeckillActivities{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultSeckillActivitiesModel) Update(ctx context.Context, data *SeckillActivities) error {
	return m.conn.WithContext(ctx).Model(&SeckillActivities{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultSeckillActivitiesModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&SeckillActivities{}).Error
}

func (m *defaultSeckillActivitiesModel) List(ctx context.Context, req SeckillActivitiesListReq) ([]SeckillActivities, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]SeckillActivities, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillActivities{})
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

func (m *defaultSeckillActivitiesModel) tableName() string {
	return m.table
}
