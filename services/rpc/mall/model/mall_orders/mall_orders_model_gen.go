// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_orders

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type (
	mallOrdersModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*MallOrders) error
		InsertOne(ctx context.Context, data *MallOrders) error
		List(ctx context.Context, req MallOrdersListReq) ([]MallOrders, int64, error)
		FindOne(ctx context.Context, id string) (*MallOrders, error)
		FindByIdempotencyKey(ctx context.Context, key string) (*MallOrders, error)
		Update(ctx context.Context, data *MallOrders) error
		Delete(ctx context.Context, id string) error
	}

	defaultMallOrdersModel struct {
		conn  *gorm.DB
		table string
	}

	MallOrders struct {
		Id             string    `json:"id" gorm:"type:text;primaryKey;comment:订单ID"`
		UserId         string    `json:"user_id" gorm:"type:text;not null;index;comment:用户ID"`
		OriginalAmount int64     `json:"original_amount" gorm:"type:bigint;not null;comment:原始金额，单位：分"`
		DiscountAmount int64     `json:"discount_amount" gorm:"type:bigint;not null;comment:优惠金额，单位：分"`
		PayAmount      int64     `json:"pay_amount" gorm:"type:bigint;not null;comment:实付金额，单位：分"`
		PayType        string    `json:"pay_type" gorm:"type:text;comment:支付方式"`
		PayTime        time.Time `json:"pay_time" gorm:"type:timestamptz;comment:支付时间"`
		OutTradeNo     string    `json:"out_trade_no" gorm:"type:text;uniqueIndex:uk_mall_orders_out_trade_no,where:out_trade_no <> '';comment:商户订单号"`
		TradeNo        string    `json:"trade_no" gorm:"type:text;uniqueIndex:uk_mall_orders_trade_no,where:trade_no <> '';comment:支付渠道交易号"`
		Remark         string    `json:"remark" gorm:"type:text;comment:用户备注"`
		Snapshot       string    `json:"snapshot" gorm:"type:jsonb;comment:订单快照"`
		Status         int       `json:"status" gorm:"type:smallint;default:1;comment:状态，1:待支付 2:已支付 3:已发货 4:已完成 5:已取消 6:退款中 7:已退款"`
		IdempotencyKey string    `json:"idempotency_key" gorm:"type:text;not null;uniqueIndex;comment:幂等键"`

		CreatedAt time.Time      `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
		UpdatedAt time.Time      `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间"`
		DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	}

	MallOrdersListReq struct {
		Page          int
		Size          int
		UserId        string
		Status        int
		PaymentStatus int
	}
)

func newMallOrdersModel(conn *gorm.DB) *defaultMallOrdersModel {
	return &defaultMallOrdersModel{
		conn:  conn,
		table: `"public"."mall_orders"`,
	}
}

func (m *defaultMallOrdersModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrders{})
}

func (m *defaultMallOrdersModel) Insert(ctx context.Context, data []*MallOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrdersModel) InsertOne(ctx context.Context, data *MallOrders) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrdersModel) FindOne(ctx context.Context, id string) (*MallOrders, error) {
	model := &MallOrders{}
	err := m.conn.WithContext(ctx).Where("id = ?", id).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultMallOrdersModel) FindByIdempotencyKey(ctx context.Context, key string) (*MallOrders, error) {
	model := &MallOrders{}
	err := m.conn.WithContext(ctx).Where("idempotency_key = ?", key).First(model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (m *defaultMallOrdersModel) Update(ctx context.Context, data *MallOrders) error {
	return m.conn.WithContext(ctx).Model(&MallOrders{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultMallOrdersModel) Delete(ctx context.Context, id string) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&MallOrders{}).Error
}

func (m *defaultMallOrdersModel) List(ctx context.Context, req MallOrdersListReq) ([]MallOrders, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 20
	}

	total := int64(0)
	list := make([]MallOrders, 0)
	session := m.conn.WithContext(ctx).Model(&MallOrders{})

	if req.UserId != "" {
		session = session.Where("user_id = ?", req.UserId)
	}
	if req.Status >= 0 {
		session = session.Where("status = ?", req.Status)
	}

	// 通过数据库字段判断支付状态
	// 支付时间可能是 NULL, 也可能是go的zeroTime, 需要同时兼容
	zeroTime := time.Time{}
	const unpaidCondition = `
		COALESCE(out_trade_no, '') = ''
		AND COALESCE(trade_no, '') = ''
		AND (pay_time IS NULL OR pay_time = ?) 
	`
	const paidCondition = `
		COALESCE(out_trade_no, '') <> ''
		AND COALESCE(trade_no, '') <> ''
		AND pay_time IS NOT NULL AND pay_time <> ?	
	`

	switch req.PaymentStatus {
	case PaymentStatusUnpaid:
		session = session.Where(unpaidCondition, zeroTime)
	case PaymentStatusPaid:
		session = session.Where(paidCondition, zeroTime)
	case PaymentStatusAbnormal:
		session = session.Where(
			"NOT (("+unpaidCondition+") OR ("+paidCondition+"))",
			zeroTime,
			zeroTime,
		)
	case -1, PaymentStatusAll:
	default:
	}

	err := session.Count(&total).Error
	if err != nil {
		return list, total, err
	}
	if total == 0 {
		return list, total, nil
	}

	offset := (req.Page - 1) * req.Size
	err = session.Order("created_at DESC").Limit(req.Size).Offset(offset).Find(&list).Error

	return list, total, err
}

func (m *defaultMallOrdersModel) tableName() string {
	return m.table
}
