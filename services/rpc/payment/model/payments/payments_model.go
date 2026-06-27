// Code scaffolded by goctl. Safe to edit.
// gorm

package payments

import (
	"gorm.io/gorm"
)

var _ PaymentsModel = (*customPaymentsModel)(nil)

type (
	PaymentsModel interface {
		paymentsModel
	}

	customPaymentsModel struct {
		*defaultPaymentsModel
	}
)

func NewPaymentsModel(conn *gorm.DB) PaymentsModel {
	return &customPaymentsModel{
		defaultPaymentsModel: newPaymentsModel(conn),
	}
}

func (m *customPaymentsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Payments{}) {
		return m.conn.Migrator().CreateTable(&Payments{})
	}
	return nil
}
