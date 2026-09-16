import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { Alert, Button, Card, Descriptions, Empty, Tag } from 'antd'
import { getOrderDetail, createPayment, type PaymentSession } from '@/api/mall'
import { PriceDisplay } from '@/components/PriceDisplay'
import { OrderItems } from '@/components/OrderItems'
import { CancelOrderButton } from '@/components/CancelOrderButton'
import { PaymentDialog } from '@/components/PaymentDialog'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { PaymentStatus } from '@/constants/paymentStatus'
import { formatDateTime, getOrderStatusText, getOrderStatusColor, OrderStatus } from '@/utils/format'

function OrderDetail({ id }: { id: string }) {
  const { state } = useLocation()
  const listTarget = typeof state?.orderList === 'string' && /^\/orders(?:\?|$)/.test(state.orderList) ? state.orderList : '/orders'
  const load = useCallback((signal: AbortSignal) => getOrderDetail(id, signal), [id])
  const { data, loading, error, reload } = useResource(load)
  const order = data?.order
  const [payment, setPayment] = useState<PaymentSession | null>(null)
  const [payLoading, setPayLoading] = useState(false)
  const [payError, setPayError] = useState('')
  const [settledStatus, setSettledStatus] = useState<number | null>(null)
  const requestRef = useRef<AbortController | null>(null)
  useEffect(() => () => requestRef.current?.abort(), [])
  const settled = useCallback((status: number) => { setSettledStatus(status); reload() }, [reload])

  const pay = async () => {
    if (requestRef.current || order?.status !== OrderStatus.PENDING || settledStatus !== null) return
    const controller = new AbortController()
    requestRef.current = controller
    setPayLoading(true)
    setPayError('')
    try {
      const result = await createPayment(id, controller.signal)
      if (!controller.signal.aborted) setPayment(result)
    } catch (err) {
      if (!controller.signal.aborted) setPayError((err as Error).message)
    } finally {
      if (!controller.signal.aborted) { requestRef.current = null; setPayLoading(false) }
    }
  }

  return <div className="commerce-page order-detail-page">
    <Link className="commerce-text-link" to={listTarget}>← 返回订单列表</Link>
    <div className="commerce-heading"><div><span className="eyebrow">你的购物记录</span><h1>订单详情</h1></div><Button loading={loading} disabled={payLoading || !!payment} onClick={reload}>刷新订单</Button></div>
    {payError && <Alert type="error" showIcon message="发起支付失败" description={payError} />}
    {settledStatus === PaymentStatus.SUCCESS && order?.status === OrderStatus.PENDING && <Alert type="info" showIcon message="已确认支付成功，订单状态同步中" description="请稍后刷新订单，勿重复付款。" />}
    {settledStatus === PaymentStatus.CLOSED && <Alert type="warning" showIcon message="该支付已关闭" description="请刷新订单确认状态。如需重新购买，请先确认原订单已取消。" />}
    <ResourceState loading={loading} error={error} retry={reload} />
    {order ? <>
      <Card>
        <div className="order-detail-heading"><Tag color={getOrderStatusColor(order.status)}>{getOrderStatusText(order.status)}</Tag>
          {order.status === OrderStatus.PENDING && <div className="order-actions">
            <CancelOrderButton orderId={order.id} disabled={payLoading || !!payment || settledStatus === PaymentStatus.SUCCESS} onCancelled={reload} />
            <Button type="primary" loading={payLoading} disabled={!!payment || settledStatus !== null} onClick={pay}>去支付</Button>
          </div>}
        </div>
        <Descriptions column={{ xs: 1, sm: 2 }} items={[
          { key: 'id', label: '订单号', children: <span className="break-anywhere">{order.id}</span>, span: 2 },
          { key: 'created', label: '创建时间', children: formatDateTime(order.created_at) },
          { key: 'updated', label: '更新时间', children: formatDateTime(order.updated_at) },
          { key: 'type', label: '支付方式', children: order.pay_type || '尚未支付' },
          { key: 'paid', label: '支付时间', children: formatDateTime(order.pay_time) },
          { key: 'remark', label: '订单备注', children: <span className="break-anywhere">{order.remark || '无'}</span>, span: 2 },
        ]} />
      </Card>
      <Card title="商品清单"><OrderItems items={order.items} />
        <div className="order-amounts"><div><span>商品原价</span><PriceDisplay cents={order.original_amount} /></div><div><span>优惠金额</span><span>− <PriceDisplay cents={order.discount_amount} /></span></div><div className="purchase-total"><span>应付金额</span><PriceDisplay cents={order.pay_amount} /></div></div>
      </Card>
    </> : data && <Empty description="订单不存在或无法访问" />}
    {payment && <PaymentDialog orderId={id} payment={payment} onClose={() => setPayment(null)} onSettled={settled} />}
  </div>
}

export default function OrderDetailPage() {
  const { id = '' } = useParams()
  // 详情地址变化时，清理原订单的支付请求和弹窗状态。
  return <OrderDetail key={id} id={id} />
}
