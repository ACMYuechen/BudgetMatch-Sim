// Code scaffolded by goctl. Safe to edit.
// gorm

package products

import (
	"gorm.io/gorm"
)

var _ ProductsModel = (*customProductsModel)(nil)

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

func (m *customProductsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Products{}) {
		return m.conn.Migrator().CreateTable(&Products{})
	}
	return nil
}
