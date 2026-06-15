// Code scaffolded by goctl. Safe to edit.
// gorm

package products

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	productsModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*Products) error
		InsertOne(ctx context.Context, data *Products) error
		List(ctx context.Context, req ProductsListReq) ([]Products, int64, error)
		FindOne(ctx context.Context, id string) (*Products, error)
		FindBySpuCode(ctx context.Context, spuCode string) (*Products, error)
		Update(ctx context.Context, data *Products) error
		Delete(ctx context.Context, id string) error
	}

	defaultProductsModel struct {
		conn  *gorm.DB
		table string
	}

	Products struct {
		Id          string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:SPU ID"`
		SpuCode     string         `json:"spu_code" gorm:"type:varchar(64);not null;uniqueIndex;comment:SPU编码"`
		Name        string         `json:"name" gorm:"type:varchar(255);not null;comment:商品名称"`
		CategoryId  string         `json:"category_id" gorm:"type:varchar(36);index;comment:分类ID"`
		Brand       string         `json:"brand" gorm:"type:varchar(100);comment:品牌"`
		Status      int64          `json:"status" gorm:"type:smallint;default:1;comment:状态，0:下架 1:上架"`
		MainImage   string         `json:"main_image" gorm:"type:varchar(500);comment:主图URL"`
		Detail      string         `json:"detail" gorm:"type:jsonb;comment:商品详情"`
		CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt   gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	ProductsListReq struct {
		Page       int
		Size       int
		CategoryId string
		Keyword    string
		Status     int64
	}
)

func newProductsModel(conn *gorm.DB) *defaultProductsModel {
	return &defaultProductsModel{
		conn:  conn,
		table: `"public"."products"`,
	}
}

func (m *defaultProductsModel) CreateTable() error {
	return m.conn.AutoMigrate(&Products{})
}

func (m *defaultProductsModel) Insert(ctx context.Context, data []*Products) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultProductsModel) InsertOne(ctx context.Context, data *Products) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultProductsModel) FindOne(ctx context.Context, id string) (*Products, error) {
	model := &Products{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultProductsModel) FindBySpuCode(ctx context.Context, spuCode string) (*Products, error) {
	model := &Products{}
	err := m.conn.WithContext(ctx).Where("spu_code = ?", spuCode).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultProductsModel) Update(ctx context.Context, data *Products) error {
	return m.conn.WithContext(ctx).Model(&Products{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultProductsModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&Products{}).Error
}

func (m *defaultProductsModel) List(ctx context.Context, req ProductsListReq) ([]Products, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]Products, 0)
	session := m.conn.WithContext(ctx).Model(&Products{})

	if req.CategoryId != "" {
		session = session.Where("category_id = ?", req.CategoryId)
	}
	if req.Keyword != "" {
		session = session.Where("name ILIKE ?", "%"+req.Keyword+"%")
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

func (m *defaultProductsModel) tableName() string {
	return m.table
}
