// Code scaffolded by goctl. Safe to edit.
// gorm

package seckill_sku

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	seckillSkusModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*SeckillSkus) error
		InsertOne(ctx context.Context, data *SeckillSkus) error
		List(ctx context.Context, req SeckillSkusListReq) ([]SeckillSkus, int64, error)
		FindOne(ctx context.Context, id string) (*SeckillSkus, error)
		Update(ctx context.Context, data *SeckillSkus) error
		Delete(ctx context.Context, id string) error
	}

	defaultSeckillSkusModel struct {
		conn  *gorm.DB
		table string
	}

	SeckillSkus struct {
		Id             string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:SKU ID"`
		ActivityId     string         `json:"activity_id" gorm:"type:varchar(36);not null;index;comment:活动ID"`
		Title          string         `json:"title" gorm:"type:varchar(255);not null;comment:SKU标题"`
		Subtitle       string         `json:"subtitle" gorm:"type:varchar(255);comment:副标题"`
		Pic            string         `json:"pic" gorm:"type:varchar(500);comment:商品图片URL"`
		OriginalPrice  int64          `json:"original_price" gorm:"type:bigint;not null;comment:原价，单位：分"`
		SeckillPrice   int64          `json:"seckill_price" gorm:"type:bigint;not null;comment:秒杀价，单位：分"`
		Stock          int64          `json:"stock" gorm:"type:int;default:0;comment:库存"`
		Sold           int64          `json:"sold" gorm:"type:int;default:0;comment:已售"`
		LockStock      int64          `json:"lock_stock" gorm:"type:int;default:0;comment:锁定库存"`
		Status         int64          `json:"status" gorm:"type:smallint;default:1;comment:状态，0:禁用 1:启用"`
		Sort           int64          `json:"sort" gorm:"type:int;default:0;comment:排序"`
		MallSkuId      string         `json:"mall_sku_id" gorm:"type:varchar(36);index;default:'';comment:关联商城SKU ID"`
		MallProductId  string         `json:"mall_product_id" gorm:"type:varchar(36);default:'';comment:关联商城商品ID"`
		CreatedAt      time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt      time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt      gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	SeckillSkusListReq struct {
		Page int `json:"page"`
		Size int `json:"size"`
	}
)

func newSeckillSkusModel(conn *gorm.DB) *defaultSeckillSkusModel {
	return &defaultSeckillSkusModel{
		conn:  conn,
		table: `"public"."seckill_skus"`,
	}
}

func (m *defaultSeckillSkusModel) CreateTable() error {
	return m.conn.AutoMigrate(&SeckillSkus{})
}

func (m *defaultSeckillSkusModel) Insert(ctx context.Context, data []*SeckillSkus) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillSkusModel) InsertOne(ctx context.Context, data *SeckillSkus) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultSeckillSkusModel) FindOne(ctx context.Context, id string) (*SeckillSkus, error) {
	model := &SeckillSkus{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultSeckillSkusModel) Update(ctx context.Context, data *SeckillSkus) error {
	return m.conn.WithContext(ctx).Model(&SeckillSkus{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultSeckillSkusModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&SeckillSkus{}).Error
}

func (m *defaultSeckillSkusModel) List(ctx context.Context, req SeckillSkusListReq) ([]SeckillSkus, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]SeckillSkus, 0)
	session := m.conn.WithContext(ctx).Model(&SeckillSkus{})
	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (req.Page - 1) * req.Size
	err = session.Order("sort ASC, created_at DESC").Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *defaultSeckillSkusModel) tableName() string {
	return m.table
}
