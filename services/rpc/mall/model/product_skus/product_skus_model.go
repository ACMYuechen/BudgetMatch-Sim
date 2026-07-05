// Code scaffolded by goctl. Safe to edit.
// gorm

package product_skus

import (
	"time"

	"gorm.io/gorm"
)

var _ ProductSkusModel = (*customProductSkusModel)(nil)

type (
	ProductSkusModel interface {
		productSkusModel

		// DeductStockTx 使用乐观锁扣减库存（stock >= quantity），扣减成功返回 true。
		DeductStockTx(tx *gorm.DB, skuID string, quantity int64, now time.Time) (bool, error)
		// RestoreStockTx 恢复库存（增加库存、减少销量）。
		RestoreStockTx(tx *gorm.DB, skuID string, quantity int64, now time.Time) error
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

// CreateTable 若表不存在则创建。
func (m *customProductSkusModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&ProductSkus{}) {
		return m.conn.Migrator().CreateTable(&ProductSkus{})
	}
	return nil
}

// DeductStockTx 使用乐观锁扣减库存（stock >= quantity），扣减成功返回 true。
func (m *customProductSkusModel) DeductStockTx(tx *gorm.DB, skuID string, quantity int64, now time.Time) (bool, error) {
	result := tx.Exec(
		"UPDATE product_skus SET stock = stock - ?, sold = sold + ?, updated_at = ? WHERE id = ? AND stock >= ?",
		quantity, quantity, now, skuID, quantity,
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// RestoreStockTx 恢复库存（增加库存、减少销量）。
func (m *customProductSkusModel) RestoreStockTx(tx *gorm.DB, skuID string, quantity int64, now time.Time) error {
	return tx.Exec(
		"UPDATE product_skus SET stock = stock + ?, sold = sold - ?, updated_at = ? WHERE id = ?",
		quantity, quantity, now, skuID,
	).Error
}
