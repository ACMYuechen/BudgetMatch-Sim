// Code scaffolded by goctl. Safe to edit.
// gorm

package products

import (
	"budgetmatch-sim/infra/uuid"

	"gorm.io/gorm"
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

func NewProductId() string {
	return uuid.NewPrefixedShortUUID("prod-")
}

func (m *customProductsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Products{}) {
		return m.conn.Migrator().CreateTable(&Products{})
	}
	return nil
}
