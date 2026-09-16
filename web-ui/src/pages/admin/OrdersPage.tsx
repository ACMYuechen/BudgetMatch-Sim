import { useCallback } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Input, Select, Table, Tag } from 'antd'
import { adminOrders } from '@/api/admin'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { readPage } from '@/utils/mall'
import { readFilter } from '@/utils/admin'
import { formatPrice, getOrderStatusText } from '@/utils/format'
import { OrderStatusText } from '@/constants/orderStatus'

export default function OrdersPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const page = readPage(params.get('page'))
  const status = readFilter(params.get('status'), [1, 2, 3, 4, 5, 6, 7])
  const payment = readFilter(params.get('payment_status'), [0, 1, 2])
  const userId = (params.get('user_id') || '').slice(0, 128)
  const load = useCallback((signal: AbortSignal) => adminOrders({ page, page_size: 10, status, payment_status: payment, user_id: userId }, signal), [page, status, payment, userId])
  const resource = useResource(load)
  const update = (key: string, value: string) => { const next = new URLSearchParams(params); next.set(key, value); if (key !== 'page') next.delete('page'); setParams(next) }
  return <><div className="commerce-heading"><div><span className="eyebrow">交易查询</span><h1>商城订单</h1><p>商城订单与秒杀订单使用独立接口，此处仅管理商城订单。</p></div><Button onClick={resource.reload}>刷新订单</Button></div><div className="admin-filters"><Input.Search key={userId} aria-label="订单用户 ID" defaultValue={userId} maxLength={128} placeholder="按用户 ID 查询" enterButton="查询" onSearch={(value) => update('user_id', value.trim())} /><Select aria-label="管理订单状态" value={status} options={[{ value: -1, label: '全部订单状态' }, ...Object.entries(OrderStatusText).filter(([key]) => key !== '0').map(([value, label]) => ({ value: Number(value), label }))]} onChange={(value) => update('status', String(value))} /><Select aria-label="支付状态筛选" value={payment} options={[{ value: -1, label: '全部支付状态' }, { value: 0, label: '待支付' }, { value: 1, label: '支付成功' }, { value: 2, label: '支付关闭' }]} onChange={(value) => update('payment_status', String(value))} /></div>
    <ResourceState {...resource} retry={resource.reload} />{resource.data && <><Table className="admin-table" rowKey="id" pagination={false} scroll={{ x: 800 }} dataSource={resource.data.list || []} columns={[
      { title: '订单号', dataIndex: 'id', render: (id) => <Link to={`/admin/orders/${encodeURIComponent(id)}`} state={{ adminList: `${location.pathname}${location.search}` }}>{id}</Link> }, { title: '用户', dataIndex: 'user_id' }, { title: '应付金额', dataIndex: 'pay_amount', render: formatPrice }, { title: '订单状态', dataIndex: 'status', render: (value) => <Tag>{getOrderStatusText(value)}</Tag> }, { title: '支付流水', render: (_, item) => item.out_trade_no ? ({ 0: '待支付', 1: '成功', 2: '已关闭' }[item.payment_status] || `未知(${item.payment_status})`) : '暂无流水' },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data.total} onChange={(value) => update('page', String(value))} /></>}
  </>
}
