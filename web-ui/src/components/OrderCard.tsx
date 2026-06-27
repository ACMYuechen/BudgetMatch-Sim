import { Card, Tag, Button, Space } from 'antd'
import { PriceDisplay } from './PriceDisplay'
import { formatDateTime, getOrderStatusText } from '@/utils/format'
import type { Order } from '@/types/api'

interface OrderCardProps {
  order: Order
  onCancel?: (order: Order) => void
  onView?: (order: Order) => void
}

const statusColors: Record<number, string> = {
  0: 'orange',
  1: 'green',
  2: 'blue',
  3: 'success',
  4: 'default',
  5: 'red',
}

export function OrderCard({ order, onCancel, onView }: OrderCardProps) {
  return (
    <Card
      title={
        <Space>
          <span>订单号: {order.id}</span>
          <Tag color={statusColors[order.status] || 'default'}>{getOrderStatusText(order.status)}</Tag>
        </Space>
      }
      extra={
        <Space>
          <Button type="link" onClick={() => onView?.(order)}>查看详情</Button>
          {order.status === 0 && (
            <Button danger type="link" onClick={() => onCancel?.(order)}>取消</Button>
          )}
        </Space>
      }
    >
      <div className="space-y-3">
        {order.items.map((item, idx) => (
          <div key={idx} className="flex justify-between items-center">
            <div>
              <div className="font-medium">{item.sku_name}</div>
              <div className="text-gray-500 text-sm">数量: {item.quantity}</div>
            </div>
            <PriceDisplay cents={item.total_amount} />
          </div>
        ))}
        <div className="flex justify-between items-center pt-3 border-t">
          <div className="text-gray-500 text-sm">创建时间: {formatDateTime(order.created_at)}</div>
          <div className="text-lg">
            总计: <PriceDisplay cents={order.total_amount} />
          </div>
        </div>
      </div>
    </Card>
  )
}
