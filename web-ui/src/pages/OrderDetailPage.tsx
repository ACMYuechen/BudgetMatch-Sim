import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, Descriptions, Button, Spin, Empty, Tag, message } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'
import { getOrderDetail, cancelOrder } from '@/api/mall'
import { PriceDisplay } from '@/components/PriceDisplay'
import { formatDateTime, getOrderStatusText } from '@/utils/format'
import type { Order } from '@/types/api'

const statusColors: Record<number, string> = {
  0: 'orange',
  1: 'green',
  2: 'blue',
  3: 'success',
  4: 'default',
  5: 'red',
}

export default function OrderDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [order, setOrder] = useState<Order | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    getOrderDetail(id)
      .then((res) => setOrder(res.order))
      .finally(() => setLoading(false))
  }, [id])

  const handleCancel = async () => {
    if (!id) return
    try {
      await cancelOrder(id)
      message.success('取消成功')
      const res = await getOrderDetail(id)
      setOrder(res.order)
    } catch (err) {
      message.error((err as Error).message)
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <Spin size="large" />
      </div>
    )
  }

  if (!order) {
    return <Empty description="订单不存在" />
  }

  return (
    <div className="space-y-6">
      <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/orders')}>
        返回订单列表
      </Button>

      <Card
        title={
          <div className="flex items-center gap-4">
            <span>订单详情</span>
            <Tag color={statusColors[order.status] || 'default'}>
              {getOrderStatusText(order.status)}
            </Tag>
          </div>
        }
        extra={
          order.status === 0 && (
            <Button danger onClick={handleCancel}>取消订单</Button>
          )
        }
      >
        <Descriptions column={2}>
          <Descriptions.Item label="订单号">{order.id}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatDateTime(order.created_at)}</Descriptions.Item>
          <Descriptions.Item label="支付方式">{order.pay_type || '-'}</Descriptions.Item>
          <Descriptions.Item label="支付时间">{formatDateTime(order.pay_time)}</Descriptions.Item>
          <Descriptions.Item label="备注">{order.remark || '-'}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatDateTime(order.updated_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="商品清单">
        <div className="space-y-4">
          {order.items.map((item, idx) => (
            <div
              key={idx}
              className="flex justify-between items-center p-4 bg-gray-50 rounded"
            >
              <div>
                <div className="font-medium">{item.sku_name}</div>
                <div className="text-gray-500 text-sm">数量: {item.quantity}</div>
              </div>
              <PriceDisplay cents={item.total_amount} />
            </div>
          ))}
        </div>

        <div className="flex justify-end items-center mt-6 pt-4 border-t text-xl">
          订单总计: <PriceDisplay cents={order.total_amount} />
        </div>
      </Card>
    </div>
  )
}
