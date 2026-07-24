// Code scaffolded by goctl. Safe to edit.
// gorm

package payments

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// 支付流水状态
const (
	StatusPending int64 = 0 // 待支付
	StatusSuccess int64 = 1 // 支付成功
	StatusClosed  int64 = 2 // 已关闭/取消
)

type (
	paymentsModel interface {
		CreateTable() error
		InsertOne(ctx context.Context, data *Payments) error
		FindOne(ctx context.Context, id string) (*Payments, error)
		FindByOutTradeNo(ctx context.Context, outTradeNo string) (*Payments, error)
		FindByOrderID(ctx context.Context, orderID string) (*Payments, error)
		Update(ctx context.Context, data *Payments) error
	}

	defaultPaymentsModel struct {
		conn  *gorm.DB
		table string
	}

	Payments struct {
		Id         string         `json:"id" gorm:"type:varchar(36);primaryKey;comment:流水ID"`
		OutTradeNo string         `json:"out_trade_no" gorm:"type:varchar(64);not null;uniqueIndex;comment:商户订单号"`
		OrderId    string         `json:"order_id" gorm:"type:varchar(36);not null;index;comment:业务订单ID"`
		UserId     string         `json:"user_id" gorm:"type:varchar(36);not null;index;comment:用户ID"`
		Amount     int64          `json:"amount" gorm:"type:bigint;not null;comment:金额，单位：分"`
		Channel    string         `json:"channel" gorm:"type:varchar(32);not null;default:alipay;comment:支付渠道"`
		Status     int64          `json:"status" gorm:"type:smallint;default:0;comment:状态，0:pending 1:success 2:closed"`
		TradeNo    string         `json:"trade_no" gorm:"type:varchar(64);index;comment:支付宝交易号"`
		BuyerId    string         `json:"buyer_id" gorm:"type:varchar(64);comment:买家支付宝用户ID"`
		NotifyRaw  string         `json:"notify_raw" gorm:"type:jsonb;comment:最近一次回调/查询原始报文"`
		QrCode     string         `json:"qr_code" gorm:"type:text;comment:支付宝二维码链接"`
		PaidAt     *time.Time     `json:"paid_at" gorm:"type:timestamptz;comment:支付完成时间"`
		CreatedAt  time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt  time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt  gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}
)

func newPaymentsModel(conn *gorm.DB) *defaultPaymentsModel {
	return &defaultPaymentsModel{
		conn:  conn,
		table: `"public"."payments"`,
	}
}

func (m *defaultPaymentsModel) CreateTable() error {
	return m.conn.AutoMigrate(&Payments{})
}

func (m *defaultPaymentsModel) InsertOne(ctx context.Context, data *Payments) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultPaymentsModel) FindOne(ctx context.Context, id string) (*Payments, error) {
	model := &Payments{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultPaymentsModel) FindByOutTradeNo(ctx context.Context, outTradeNo string) (*Payments, error) {
	model := &Payments{}
	err := m.conn.WithContext(ctx).Where("out_trade_no = ?", outTradeNo).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

// FindByOrderID 返回该订单最近一笔支付流水。
func (m *defaultPaymentsModel) FindByOrderID(ctx context.Context, orderID string) (*Payments, error) {
	model := &Payments{}
	err := m.conn.WithContext(ctx).Where("order_id = ?", orderID).Order("created_at DESC").First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultPaymentsModel) Update(ctx context.Context, data *Payments) error {
	return m.conn.WithContext(ctx).Save(data).Error
}
