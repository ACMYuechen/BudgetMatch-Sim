// Code scaffolded by goctl. Safe to edit.
// gorm

package product_skus

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	productSkusModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*ProductSkus) error
		InsertOne(ctx context.Context, data *ProductSkus) error
		List(ctx context.Context, req ProductSkusListReq) ([]ProductSkus, int64, error)
		FindOne(ctx context.Context, id string) (*ProductSkus, error)
		Update(ctx context.Context, data *ProductSkus) error
		Delete(ctx context.Context, id string) error
	}

	defaultProductSkusModel struct {
		conn  *gorm.DB
		table string
	}

	ProductSkus struct {
		Id           string `json:"id" gorm:"type:text;primaryKey;comment:SKU ID"`
		ProductId    string `json:"product_id" gorm:"type:text;not null;index;comment:SPU ID"`
		Name         string `json:"name" gorm:"type:text;not null;comment:SKU名称"`
		Specs        string `json:"specs" gorm:"type:jsonb;comment:规格JSON"`
		Price        int64  `json:"price" gorm:"type:bigint;not null;comment:售价，单位：分"`
		Stock        int    `json:"stock" gorm:"type:int;default:0;comment:库存"`
		Sold         int    `json:"sold" gorm:"type:int;default:0;comment:已售"`
		Status       int    `json:"status" gorm:"type:smallint;default:1;comment:状态，0:下架 1:上架"`
		AgentComment string `json:"agent_comment" gorm:"type:jsonb;comment:Agent推理核心: 大模型对sku的描述与评价"`

		CreatedAt time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	ProductSkusListReq struct {
		Page      int
		Size      int
		ProductId string
		Status    int
	}
)

func newProductSkusModel(conn *gorm.DB) *defaultProductSkusModel {
	return &defaultProductSkusModel{
		conn:  conn,
		table: `"public"."product_skus"`,
	}
}

func (m *defaultProductSkusModel) CreateTable() error {
	return m.conn.AutoMigrate(&ProductSkus{})
}

func (m *defaultProductSkusModel) Insert(ctx context.Context, data []*ProductSkus) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultProductSkusModel) InsertOne(ctx context.Context, data *ProductSkus) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultProductSkusModel) FindOne(ctx context.Context, id string) (*ProductSkus, error) {
	model := &ProductSkus{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultProductSkusModel) Update(ctx context.Context, data *ProductSkus) error {
	return m.conn.WithContext(ctx).Model(&ProductSkus{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultProductSkusModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&ProductSkus{}).Error
}

func (m *defaultProductSkusModel) List(ctx context.Context, req ProductSkusListReq) ([]ProductSkus, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]ProductSkus, 0)
	session := m.conn.WithContext(ctx).Model(&ProductSkus{}).Where("product_id = ?", req.ProductId)

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

func (m *defaultProductSkusModel) tableName() string {
	return m.table
}
