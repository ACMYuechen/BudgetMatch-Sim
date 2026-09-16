import { Link } from 'react-router-dom'
import { Card, Tag } from 'antd'
import { PriceDisplay } from './PriceDisplay'
import { OrderItems } from './OrderItems'
import { CancelOrderButton } from './CancelOrderButton'
import { formatDateTime, getOrderStatusText, getOrderStatusColor, OrderStatus } from '@/utils/format'
import type { Order } from '@/types/api'

export function OrderCard({ order, returnTo, onCancelled }: { order: Order; returnTo: string; onCancelled: () => void }) {
  return <Card className="order-card">
    <div className="order-card-heading"><div><span className="commerce-note">订单号</span><p className="break-anywhere">{order.id}</p></div><Tag color={getOrderStatusColor(order.status)}>{getOrderStatusText(order.status)}</Tag></div>
    <OrderItems items={order.items} />
    <div className="order-card-total"><span className="commerce-note">{formatDateTime(order.created_at)}</span><span>应付金额 <PriceDisplay cents={order.pay_amount} /></span></div>
    <div className="order-actions">{order.status === OrderStatus.PENDING && <CancelOrderButton orderId={order.id} onCancelled={onCancelled} />}<Link className="order-detail-link" to={`/orders/${encodeURIComponent(order.id)}`} state={{ orderList: returnTo }}>{order.status === OrderStatus.PENDING ? '查看并支付' : '查看详情'} →</Link></div>
  </Card>
}
