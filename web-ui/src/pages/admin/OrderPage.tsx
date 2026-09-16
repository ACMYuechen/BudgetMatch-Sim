import { useCallback } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { Alert, Button, Card, Descriptions, Tag } from 'antd'
import { adminOrder, updateOrderStatus } from '@/api/admin'
import { ConfirmAction } from '@/components/admin/AdminActions'
import { ResourceState } from '@/components/ResourceState'
import { OrderItems } from '@/components/OrderItems'
import { useResource } from '@/hooks/useResource'
import { formatDateTime, formatPrice, getOrderStatusText } from '@/utils/format'

const actions: Record<number, { label: string; next: number; description: string }> = {
  1: { label: '取消待支付订单', next: 5, description: '取消待支付订单会触发库存回补。请确认没有正在处理的支付，服务端将再次校验状态。' },
  2: { label: '标记已发货', next: 3, description: '请确认已完成实际发货。此操作仅变更订单状态，不会自动创建物流单。' },
  3: { label: '标记已完成', next: 4, description: '请确认订单已实际履约完成。此操作不可通过此页面撤销。' },
}
export default function OrderPage() {
  const { id = '' } = useParams()
  const { state } = useLocation()
  const back = typeof state?.adminList === 'string' && /^\/admin\/orders(?:\?|$)/.test(state.adminList) ? state.adminList : '/admin/orders'
  const load = useCallback(async (signal: AbortSignal) => {
    const data = await adminOrder(id, signal)
    if (!data.order || data.order.id !== id) throw new Error('订单不存在或响应不匹配')
    return data.order
  }, [id])
  const resource = useResource(load)
  const order = resource.data
  const action = order && actions[order.status]
  return <><Link className="commerce-text-link" to={back}>← 返回订单管理</Link><ResourceState {...resource} retry={resource.reload} />{order && <><div className="commerce-heading"><div><h1>订单详情</h1><p className="break-anywhere">{order.id}</p><Tag>{getOrderStatusText(order.status)}</Tag></div><div className="admin-actions"><Button onClick={resource.reload}>刷新订单</Button>{action && <ConfirmAction key={`${id}:${order.status}`} label={action.label} danger={action.next === 5} description={action.description} action={(signal) => updateOrderStatus(id, action.next, signal)} onSaved={resource.reload} />}</div></div>
    <Alert type="info" message="不提供手工标记付款或退款入口" description="修改订单状态不等于真实资金到账或退款；支付与退款应由对应支付链路确认。" />
    <Card><Descriptions column={{ xs: 1, md: 2 }}><Descriptions.Item label="用户">{order.user_id}</Descriptions.Item><Descriptions.Item label="应付金额">{formatPrice(order.pay_amount)}</Descriptions.Item><Descriptions.Item label="原始金额">{formatPrice(order.original_amount)}</Descriptions.Item><Descriptions.Item label="优惠">{formatPrice(order.discount_amount)}</Descriptions.Item><Descriptions.Item label="支付流水状态">{order.out_trade_no ? ({ 0: '待支付', 1: '支付成功', 2: '已关闭' }[order.payment_status] || `未知(${order.payment_status})`) : '暂无流水'}</Descriptions.Item><Descriptions.Item label="商户流水号">{order.out_trade_no || '—'}</Descriptions.Item><Descriptions.Item label="支付渠道流水">{order.trade_no || '—'}</Descriptions.Item><Descriptions.Item label="创建时间">{formatDateTime(order.created_at)}</Descriptions.Item><Descriptions.Item label="备注">{order.remark || '—'}</Descriptions.Item></Descriptions><OrderItems items={order.items || []} /><Link className="commerce-text-link" to={`/admin/outbox?${new URLSearchParams({ aggregate_id: id })}`}>查看关联消息事件 →</Link></Card>
  </>}</>
}
