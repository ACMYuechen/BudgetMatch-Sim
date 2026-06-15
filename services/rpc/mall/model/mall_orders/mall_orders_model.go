// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_orders

import (
	"gorm.io/gorm"
)

var _ MallOrdersModel = (*customMallOrdersModel)(nil)

type (
	MallOrdersModel interface {
		mallOrdersModel
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

func (m *customMallOrdersModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&MallOrders{}) {
		return m.conn.Migrator().CreateTable(&MallOrders{})
	}
	return nil
}
