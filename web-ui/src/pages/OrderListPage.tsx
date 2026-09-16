import { useCallback } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Empty, Tabs } from 'antd'
import { getOrderList } from '@/api/mall'
import { OrderCard } from '@/components/OrderCard'
import { Pagination } from '@/components/Pagination'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { OrderStatusText } from '@/constants/orderStatus'
import { readPage, readPageSize } from '@/utils/mall'

export default function OrderListPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const rawStatus = params.get('status')
  const status = rawStatus !== null && /^[1-7]$/.test(rawStatus) ? Number(rawStatus) : -1
  const page = readPage(params.get('page'))
  const pageSize = readPageSize(params.get('page_size'), [10, 20, 50])
  const load = useCallback((signal: AbortSignal) => getOrderList({ page, page_size: pageSize, status }, signal), [page, pageSize, status])
  const { data, loading, error, reload } = useResource(load)
  const updateQuery = (nextPage: number, nextSize = pageSize, nextStatus = status) => {
    const next = new URLSearchParams()
    if (nextStatus !== -1) next.set('status', String(nextStatus))
    if (nextPage > 1) next.set('page', String(nextPage))
    if (nextSize !== 10) next.set('page_size', String(nextSize))
    setParams(next)
  }
  return <div className="commerce-page">
    <div className="commerce-heading"><div><span className="eyebrow">每一笔，都心中有数</span><h1>我的订单</h1><p>查看购物进度，继续未完成的计划。</p></div><Button onClick={reload} loading={loading}>刷新订单</Button></div>
    <Tabs activeKey={String(status)} onChange={(key) => updateQuery(1, pageSize, Number(key))} items={[{ key: '-1', label: '全部' }, ...Object.entries(OrderStatusText).filter(([key]) => key !== '0').map(([key, label]) => ({ key, label }))]} />
    <ResourceState loading={loading} error={error} retry={reload} />
    {data && <>
      <p className="list-summary" role="status">共 {data.total} 笔订单</p>
      {data.list?.length ? <div className="orders-grid">{data.list.map((order) => <OrderCard key={order.id} order={order} returnTo={`${location.pathname}${location.search}`} onCancelled={reload} />)}</div>
        : <Empty description={page > 1 ? '这一页没有订单了' : status === -1 ? '还没有订单，去找点喜欢的吧' : `暂时没有${OrderStatusText[status]}的订单`}>
          {page > 1 ? <Button onClick={() => updateQuery(1)}>回到第一页</Button> : <Link className="commerce-text-link" to="/products">去逛逛商品 →</Link>}
        </Empty>}
      <Pagination page={page} pageSize={pageSize} total={data.total} onChange={(p, size) => updateQuery(size === pageSize ? p : 1, size)} />
    </>}
  </div>
}
