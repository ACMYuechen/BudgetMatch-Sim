// 订单状态常量 - 与后端 protobuf 定义保持一致
// 参考: services/rpc/mall/proto/mall.proto OrderStatus enum

export const OrderStatus = {
  UNSPECIFIED: 0,
  PENDING: 1,      // 待支付
  PAID: 2,         // 已支付
  SHIPPED: 3,      // 已发货
  COMPLETED: 4,    // 已完成
  CANCELLED: 5,    // 已取消
  REFUNDING: 6,    // 退款中
  REFUNDED: 7,     // 已退款
} as const

export const OrderStatusText: Record<number, string> = {
  [OrderStatus.UNSPECIFIED]: '未知',
  [OrderStatus.PENDING]: '待支付',
  [OrderStatus.PAID]: '已支付',
  [OrderStatus.SHIPPED]: '已发货',
  [OrderStatus.COMPLETED]: '已完成',
  [OrderStatus.CANCELLED]: '已取消',
  [OrderStatus.REFUNDING]: '退款中',
  [OrderStatus.REFUNDED]: '已退款',
}

export const OrderStatusColor: Record<number, string> = {
  [OrderStatus.UNSPECIFIED]: 'default',
  [OrderStatus.PENDING]: 'orange',
  [OrderStatus.PAID]: 'green',
  [OrderStatus.SHIPPED]: 'blue',
  [OrderStatus.COMPLETED]: 'cyan',
  [OrderStatus.CANCELLED]: 'default',
  [OrderStatus.REFUNDING]: 'gold',
  [OrderStatus.REFUNDED]: 'red',
}

export function getOrderStatusText(status: number): string {
  return OrderStatusText[status] ?? `未知(${status})`
}

export function getOrderStatusColor(status: number): string {
  return OrderStatusColor[status] ?? 'default'
}
