package orderservicelogic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"budgetmatch-sim/services/rpc/mall/model/mall_orders"
)

func TestIsValidOrderTransition(t *testing.T) {
	// 待支付 -> 已支付 / 已取消
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusPending, mall_orders.OrderStatusPaid))
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusPending, mall_orders.OrderStatusCancelled))
	assert.False(t, isValidOrderTransition(mall_orders.OrderStatusPending, mall_orders.OrderStatusShipped))

	// 已支付 -> 已发货 / 已取消
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusPaid, mall_orders.OrderStatusShipped))
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusPaid, mall_orders.OrderStatusCancelled))
	assert.False(t, isValidOrderTransition(mall_orders.OrderStatusPaid, mall_orders.OrderStatusPaid))

	// 已发货 -> 已完成 / 退款中
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusShipped, mall_orders.OrderStatusCompleted))
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusShipped, mall_orders.OrderStatusRefunding))

	// 退款中 -> 已退款
	assert.True(t, isValidOrderTransition(mall_orders.OrderStatusRefunding, mall_orders.OrderStatusRefunded))

	// 终态
	assert.False(t, isValidOrderTransition(mall_orders.OrderStatusCompleted, mall_orders.OrderStatusPaid))
	assert.False(t, isValidOrderTransition(mall_orders.OrderStatusCancelled, mall_orders.OrderStatusPaid))
	assert.False(t, isValidOrderTransition(mall_orders.OrderStatusRefunded, mall_orders.OrderStatusPaid))
}
