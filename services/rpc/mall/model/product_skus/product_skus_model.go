// Code scaffolded by goctl. Safe to edit.
// gorm

package product_skus

import (
	"gorm.io/gorm"
)

var _ ProductSkusModel = (*customProductSkusModel)(nil)

type (
	ProductSkusModel interface {
		productSkusModel
	}

	customProductSkusModel struct {
		*defaultProductSkusModel
	}
)

func NewProductSkusModel(conn *gorm.DB) ProductSkusModel {
	return &customProductSkusModel{
		defaultProductSkusModel: newProductSkusModel(conn),
	}
}

func (m *customProductSkusModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&ProductSkus{}) {
		if err := m.conn.Migrator().CreateTable(&ProductSkus{}); err != nil {
			return err
		}
	}
	// 确保唯一索引存在
	if !m.conn.Migrator().HasIndex(&ProductSkus{}, "idx_product_skus_product_sku") {
		return m.conn.Exec(
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_product_skus_product_sku ON product_skus(product_id, sku_code) WHERE deleted_at IS NULL`,
		).Error
	}
	return nil
}
