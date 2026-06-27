import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Spin, Empty, message } from 'antd'
import { getOrderList, cancelOrder } from '@/api/mall'
import { OrderCard } from '@/components/OrderCard'
import { Pagination } from '@/components/Pagination'
import type { Order } from '@/types/api'

export default function OrderListPage() {
  const navigate = useNavigate()
  const [orders, setOrders] = useState<Order[]>([])
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [total, setTotal] = useState(0)

  const fetchOrders = async (p = page, ps = pageSize) => {
    setLoading(true)
    try {
      const res = await getOrderList({ page: p, page_size: ps })
      setOrders(res.list || [])
      setTotal(res.total || 0)
      setPage(res.page)
      setPageSize(res.page_size)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchOrders()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleCancel = async (order: Order) => {
    try {
      await cancelOrder(order.id)
      message.success('取消成功')
      fetchOrders()
    } catch (err) {
      message.error((err as Error).message)
    }
  }

  const handleView = (order: Order) => {
    navigate(`/orders/${order.id}`)
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">我的订单</h1>

      {loading ? (
        <div className="flex justify-center py-20">
          <Spin size="large" />
        </div>
      ) : orders.length === 0 ? (
        <Empty description="暂无订单" />
      ) : (
        <>
          <div className="space-y-4">
            {orders.map((order) => (
              <OrderCard
                key={order.id}
                order={order}
                onCancel={handleCancel}
                onView={handleView}
              />
            ))}
          </div>
          <Pagination
            page={page}
            pageSize={pageSize}
            total={total}
            onChange={fetchOrders}
          />
        </>
      )}
    </div>
  )
}
