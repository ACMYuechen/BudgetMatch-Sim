// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_orders

import (
	"time"

	infrauuid "budgetmatch-sim/infra/uuid"

	"gorm.io/gorm"
)

const idPrefix = "mord"

var (
	_          MallOrdersModel = (*customMallOrdersModel)(nil)
	generateID                 = infrauuid.MustNewPrefixedShortGenerator(idPrefix)
)

type (
	MallOrdersModel interface {
		mallOrdersModel

		// InsertTx 在事务中插入订单。
		InsertTx(tx *gorm.DB, data *MallOrders) error
		// UpdateStatusTx 使用乐观锁条件更新订单状态，返回是否实际更新成功。
		UpdateStatusTx(tx *gorm.DB, id, userID string, fromStatus, toStatus int, now time.Time) (bool, error)
		// MarkPaidTx 使用乐观锁将订单从 fromStatus 标记为已支付，并写入支付方式与支付时间，返回是否实际更新成功。
		MarkPaidTx(tx *gorm.DB, id, userID string, fromStatus int, payType string, now time.Time) (bool, error)
	}

	customMallOrdersModel struct {
		*defaultMallOrdersModel
	}
)

func NewMallOrdersModel(conn *gorm.DB) MallOrdersModel {
	return &customMallOrdersModel{
		defaultMallOrdersModel: newMallOrdersModel(conn),
	}
}

// NewID 生成商城订单表主键。
func NewID() string {
	return generateID()
}

// BeforeCreate 在写入商城订单前补充主键，保留调用方传入的已有主键。
func (o *MallOrders) BeforeCreate(_ *gorm.DB) error {
	if o.Id == "" {
		o.Id = NewID()
	}
	return nil
}

// CreateTable 若表不存在则创建。
func (m *customMallOrdersModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&MallOrders{}) {
		return m.conn.Migrator().CreateTable(&MallOrders{})
	}
	return nil
}

// InsertTx 在事务中插入订单。
func (m *customMallOrdersModel) InsertTx(tx *gorm.DB, data *MallOrders) error {
	return tx.Create(data).Error
}

// UpdateStatusTx 使用乐观锁条件更新订单状态，返回是否实际更新成功。
func (m *customMallOrdersModel) UpdateStatusTx(tx *gorm.DB, id, userID string, fromStatus, toStatus int, now time.Time) (bool, error) {
	result := tx.Model(&MallOrders{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, fromStatus).
		Updates(map[string]any{
			"status":     toStatus,
			"updated_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkPaidTx 使用乐观锁将订单从 fromStatus 标记为已支付，并写入支付方式与支付时间，返回是否实际更新成功。
func (m *customMallOrdersModel) MarkPaidTx(tx *gorm.DB, id, userID string, fromStatus int, payType string, now time.Time) (bool, error) {
	result := tx.Model(&MallOrders{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, fromStatus).
		Updates(map[string]any{
			"status":     OrderStatusPaid,
			"pay_type":   payType,
			"pay_time":   now,
			"updated_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
