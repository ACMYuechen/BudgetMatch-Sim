// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_order_items

import (
	infrauuid "budgetmatch-sim/infra/uuid"

	"gorm.io/gorm"
)

const idPrefix = "mitem"

var (
	_          MallOrderItemsModel = (*customMallOrderItemsModel)(nil)
	generateID                     = infrauuid.MustNewPrefixedShortGenerator(idPrefix)
)

type (
	MallOrderItemsModel interface {
		mallOrderItemsModel

		// InsertTx 在事务中插入订单项。
		InsertTx(tx *gorm.DB, data *MallOrderItems) error
		// InsertBatchTx 在事务中批量插入订单项。
		InsertBatchTx(tx *gorm.DB, data []*MallOrderItems) error
	}

	customMallOrderItemsModel struct {
		*defaultMallOrderItemsModel
	}
)

func NewMallOrderItemsModel(conn *gorm.DB) MallOrderItemsModel {
	return &customMallOrderItemsModel{
		defaultMallOrderItemsModel: newMallOrderItemsModel(conn),
	}
}

// NewID 生成商城订单项表主键。
func NewID() string {
	return generateID()
}

// BeforeCreate 在写入商城订单项前补充主键，保留调用方传入的已有主键。
func (i *MallOrderItems) BeforeCreate(_ *gorm.DB) error {
	if i.Id == "" {
		i.Id = NewID()
	}
	return nil
}

// CreateTable 若表不存在则创建。
func (m *customMallOrderItemsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&MallOrderItems{}) {
		return m.conn.Migrator().CreateTable(&MallOrderItems{})
	}
	return nil
}

// InsertTx 在事务中插入订单项。
func (m *customMallOrderItemsModel) InsertTx(tx *gorm.DB, data *MallOrderItems) error {
	return tx.Create(data).Error
}

// InsertBatchTx 在事务中批量插入订单项。
func (m *customMallOrderItemsModel) InsertBatchTx(tx *gorm.DB, data []*MallOrderItems) error {
	return tx.Create(data).Error
}
