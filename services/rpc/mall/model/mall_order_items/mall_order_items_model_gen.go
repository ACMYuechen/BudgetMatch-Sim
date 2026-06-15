// Code scaffolded by goctl. Safe to edit.
// gorm

package mall_order_items

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type (
	mallOrderItemsModel interface {
		CreateTable() error
		Insert(ctx context.Context, data []*MallOrderItems) error
		InsertOne(ctx context.Context, data *MallOrderItems) error
		FindByOrderId(ctx context.Context, orderId string) ([]MallOrderItems, error)
		Update(ctx context.Context, data *MallOrderItems) error
		Delete(ctx context.Context, id int64) error
	}

	defaultMallOrderItemsModel struct {
		conn  *gorm.DB
		table string
	}

	MallOrderItems struct {
		Id          int64     `json:"id" gorm:"type:bigserial;primaryKey;comment:自增ID"`
		OrderId     string    `json:"order_id" gorm:"type:varchar(36);not null;index;comment:订单ID"`
		ProductId   string    `json:"product_id" gorm:"type:varchar(36);not null;comment:SPU ID"`
		SkuId       string    `json:"sku_id" gorm:"type:varchar(36);not null;comment:SKU ID"`
		SkuName     string    `json:"sku_name" gorm:"type:varchar(255);not null;comment:SKU快照名称"`
		Price       int64     `json:"price" gorm:"type:bigint;not null;comment:单价，单位：分"`
		Quantity    int64     `json:"quantity" gorm:"type:int;not null;comment:数量"`
		TotalAmount int64     `json:"total_amount" gorm:"type:bigint;not null;comment:小计，单位：分"`
		Snapshot    string    `json:"snapshot" gorm:"type:jsonb;comment:商品快照"`
		CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间"`
	}
)

func newMallOrderItemsModel(conn *gorm.DB) *defaultMallOrderItemsModel {
	return &defaultMallOrderItemsModel{
		conn:  conn,
		table: `"public"."mall_order_items"`,
	}
}

func (m *defaultMallOrderItemsModel) CreateTable() error {
	return m.conn.AutoMigrate(&MallOrderItems{})
}

func (m *defaultMallOrderItemsModel) Insert(ctx context.Context, data []*MallOrderItems) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrderItemsModel) InsertOne(ctx context.Context, data *MallOrderItems) error {
	return m.conn.WithContext(ctx).Create(data).Error
}

func (m *defaultMallOrderItemsModel) FindByOrderId(ctx context.Context, orderId string) ([]MallOrderItems, error) {
	list := make([]MallOrderItems, 0)
	err := m.conn.WithContext(ctx).Where("order_id = ?", orderId).Find(&list).Error
	return list, err
}

func (m *defaultMallOrderItemsModel) Update(ctx context.Context, data *MallOrderItems) error {
	return m.conn.WithContext(ctx).Model(&MallOrderItems{}).Where("id = ?", data.Id).Updates(data).Error
}

func (m *defaultMallOrderItemsModel) Delete(ctx context.Context, id int64) error {
	return m.conn.WithContext(ctx).Where("id = ?", id).Delete(&MallOrderItems{}).Error
}

func (m *defaultMallOrderItemsModel) tableName() string {
	return m.table
}
