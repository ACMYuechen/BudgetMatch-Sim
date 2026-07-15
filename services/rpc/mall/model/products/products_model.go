// Code scaffolded by goctl. Safe to edit.
// gorm

package products

import (
	infrauuid "budgetmatch-sim/infra/uuid"

	"gorm.io/gorm"
)

const idPrefix = "prod"

var (
	_          ProductsModel = (*customProductsModel)(nil)
	generateID               = infrauuid.MustNewPrefixedShortGenerator(idPrefix)
)

type (
	ProductsModel interface {
		productsModel
	}

	customProductsModel struct {
		*defaultProductsModel
	}
)

func NewProductsModel(conn *gorm.DB) ProductsModel {
	return &customProductsModel{
		defaultProductsModel: newProductsModel(conn),
	}
}

// NewID 生成商品表主键。
func NewID() string {
	return generateID()
}

// BeforeCreate 在写入商品记录前补充主键，保留调用方传入的已有主键。
func (p *Products) BeforeCreate(_ *gorm.DB) error {
	if p.Id == "" {
		p.Id = NewID()
	}
	return nil
}

func (m *customProductsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Products{}) {
		return m.conn.Migrator().CreateTable(&Products{})
	}
	return nil
}
