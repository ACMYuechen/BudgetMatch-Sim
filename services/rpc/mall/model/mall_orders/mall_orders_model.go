// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_orders

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"budgetmatch-sim/infra/uuid"
)

type (
	MallOrdersModel interface {
		mallOrdersModel

		// InsertTx 在事务中插入订单。
		InsertTx(tx *gorm.DB, data *MallOrders) error
		// FindOneForUpdateTx 在事务中查询订单并加行锁。
		FindOneForUpdateTx(tx *gorm.DB, id string) (*MallOrders, error)
		// UpdateStatusTx 使用乐观锁条件更新订单状态，返回是否实际更新成功。
		UpdateStatusTx(tx *gorm.DB, id, userId string, fromStatus, toStatus int, now time.Time) (bool, error)
		// MarkPaidTx 使用乐观锁将订单从 fromStatus 标记为已支付，并写入支付方式与支付时间，返回是否实际更新成功。
		MarkPaidTx(tx *gorm.DB, id, userId string, fromStatus int, payType string, now time.Time) (bool, error)
		// ConfirmPaymentTx 原子地确认支付，并写入本次支付的唯一标识。
		ConfirmPaymentTx(tx *gorm.DB, req *ConfirmPaymentTxReq) (bool, error)
	}

	customMallOrdersModel struct {
		*defaultMallOrdersModel
	}

	ConfirmPaymentTxReq struct {
		Id         string
		UserId     string
		OutTradeNo string
		TradeNo    string
		Now        time.Time
	}
)

func NewMallOrdersModel(conn *gorm.DB) MallOrdersModel {
	return &customMallOrdersModel{
		defaultMallOrdersModel: newMallOrdersModel(conn),
	}
}

func NewMallOrderId() string {
	return uuid.NewPrefixedShortUUID("mord-")
}

// CreateTable 创建或迁移订单表。
func (m *customMallOrdersModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrders{})
}

// InsertTx 在事务中插入订单。
func (m *customMallOrdersModel) InsertTx(tx *gorm.DB, data *MallOrders) error {
	return tx.Create(data).Error
}

// FindOneForUpdateTx 在事务中查询订单并加行锁。
func (m *customMallOrdersModel) FindOneForUpdateTx(tx *gorm.DB, id string) (*MallOrders, error) {
	order := &MallOrders{}
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		First(order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return order, nil
}

// UpdateStatusTx 使用乐观锁条件更新订单状态，返回是否实际更新成功。
func (m *customMallOrdersModel) UpdateStatusTx(tx *gorm.DB, id, userId string, fromStatus, toStatus int, now time.Time) (bool, error) {
	result := tx.Model(&MallOrders{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userId, fromStatus).
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
func (m *customMallOrdersModel) MarkPaidTx(tx *gorm.DB, id, userId string, fromStatus int, payType string, now time.Time) (bool, error) {
	result := tx.Model(&MallOrders{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userId, fromStatus).
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

// ConfirmPaymentTx 原子地把待支付订单标记为已支付，并记录本次支付的唯一标识。
func (m *customMallOrdersModel) ConfirmPaymentTx(tx *gorm.DB, req *ConfirmPaymentTxReq) (bool, error) {
	id, userId, outTradeNo, tradeNo, now := req.Id, req.UserId, req.OutTradeNo, req.TradeNo, req.Now
	result := tx.Model(&MallOrders{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userId, OrderStatusPending).
		Updates(map[string]any{
			"status":       OrderStatusPaid,
			"pay_time":     now,
			"out_trade_no": outTradeNo,
			"trade_no":     tradeNo,
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
