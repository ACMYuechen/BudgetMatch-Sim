package mall_orders

// 订单状态常量
const (
	OrderStatusPending   int64 = 0 // 待支付
	OrderStatusPaid      int64 = 1 // 已支付
	OrderStatusShipped   int64 = 2 // 已发货
	OrderStatusCompleted int64 = 3 // 已完成
	OrderStatusCancelled int64 = 4 // 已取消
	OrderStatusRefunding int64 = 5 // 退款中
	OrderStatusRefunded  int64 = 6 // 已退款
)
