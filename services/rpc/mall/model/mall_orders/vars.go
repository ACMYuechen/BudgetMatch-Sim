package mall_orders

// 订单状态常量
const (
	OrderStatusPending   int = 1 // 待支付
	OrderStatusPaid      int = 2 // 已支付
	OrderStatusShipped   int = 3 // 已发货
	OrderStatusCompleted int = 4 // 已完成
	OrderStatusCancelled int = 5 // 已取消
	OrderStatusRefunding int = 6 // 退款中
	OrderStatusRefunded  int = 7 // 已退款
)
