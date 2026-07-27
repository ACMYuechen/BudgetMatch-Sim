package mall_orders

import "budgetmatch-sim/services/rpc/mall/pb"

// 数据库存储使用 int，状态值以 protobuf 契约为准。
const (
	OrderStatusPending   = int(pb.OrderStatus_ORDER_STATUS_PENDING)
	OrderStatusPaid      = int(pb.OrderStatus_ORDER_STATUS_PAID)
	OrderStatusShipped   = int(pb.OrderStatus_ORDER_STATUS_SHIPPED)
	OrderStatusCompleted = int(pb.OrderStatus_ORDER_STATUS_COMPLETED)
	OrderStatusCancelled = int(pb.OrderStatus_ORDER_STATUS_CANCELLED)
	OrderStatusRefunding = int(pb.OrderStatus_ORDER_STATUS_REFUNDING)
	OrderStatusRefunded  = int(pb.OrderStatus_ORDER_STATUS_REFUNDED)
)
