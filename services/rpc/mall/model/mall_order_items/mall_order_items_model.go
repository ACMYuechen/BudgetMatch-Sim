// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_order_items

import (
	"gorm.io/gorm"
)

var _ MallOrderItemsModel = (*customMallOrderItemsModel)(nil)

type (
	MallOrderItemsModel interface {
		mallOrderItemsModel
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

func (m *customMallOrderItemsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&MallOrderItems{}) {
		return m.conn.Migrator().CreateTable(&MallOrderItems{})
	}
	return nil
}
