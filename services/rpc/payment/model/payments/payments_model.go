// Code scaffolded by goctl. Safe to edit.
// gorm

package payments

import (
	infrauuid "budgetmatch-sim/infra/uuid"

	"gorm.io/gorm"
)

const idPrefix = "pay"

var (
	_          PaymentsModel = (*customPaymentsModel)(nil)
	generateID               = infrauuid.MustNewPrefixedShortGenerator(idPrefix)
)

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

// NewID 生成支付流水表主键。
func NewID() string {
	return generateID()
}

// BeforeCreate 在写入支付流水前补充主键，保留调用方传入的已有主键。
func (p *Payments) BeforeCreate(_ *gorm.DB) error {
	if p.Id == "" {
		p.Id = NewID()
	}
	return nil
}

func (m *customPaymentsModel) CreateTable() error {
	if !m.conn.Migrator().HasTable(&Payments{}) {
		return m.conn.Migrator().CreateTable(&Payments{})
	}
	return nil
}
