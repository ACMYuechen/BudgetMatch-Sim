package orderservicelogic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidOrderTransition(t *testing.T) {
	// pending -> paid / cancelled
	assert.True(t, isValidOrderTransition(OrderStatusPending, OrderStatusPaid))
	assert.True(t, isValidOrderTransition(OrderStatusPending, OrderStatusCancelled))
	assert.False(t, isValidOrderTransition(OrderStatusPending, OrderStatusShipped))

	// paid -> shipped / cancelled
	assert.True(t, isValidOrderTransition(OrderStatusPaid, OrderStatusShipped))
	assert.True(t, isValidOrderTransition(OrderStatusPaid, OrderStatusCancelled))
	assert.False(t, isValidOrderTransition(OrderStatusPaid, OrderStatusPaid))

	// shipped -> completed / refunding
	assert.True(t, isValidOrderTransition(OrderStatusShipped, OrderStatusCompleted))
	assert.True(t, isValidOrderTransition(OrderStatusShipped, OrderStatusRefunding))

	// refunding -> refunded
	assert.True(t, isValidOrderTransition(OrderStatusRefunding, OrderStatusRefunded))

	// terminal states
	assert.False(t, isValidOrderTransition(OrderStatusCompleted, OrderStatusPaid))
	assert.False(t, isValidOrderTransition(OrderStatusCancelled, OrderStatusPaid))
	assert.False(t, isValidOrderTransition(OrderStatusRefunded, OrderStatusPaid))
}
