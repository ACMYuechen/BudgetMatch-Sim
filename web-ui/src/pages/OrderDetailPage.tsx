import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, Descriptions, Button, Spin, Empty, Tag, message, Modal, QRCode, Space } from 'antd'
import { ArrowLeftOutlined, PayCircleOutlined } from '@ant-design/icons'
import { getOrderDetail, cancelOrder, createPayment, queryPayment } from '@/api/mall'
import { PriceDisplay } from '@/components/PriceDisplay'
import { formatDateTime, getOrderStatusText, getOrderStatusColor, OrderStatus } from '@/utils/format'
import type { Order } from '@/types/api'

export default function OrderDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [order, setOrder] = useState<Order | null>(null)
  const [loading, setLoading] = useState(false)
  const [payModalOpen, setPayModalOpen] = useState(false)
  const [payLoading, setPayLoading] = useState(false)
  const [qrCode, setQrCode] = useState('')
  const [outTradeNo, setOutTradeNo] = useState('')
  const paymentCheckTimer = useRef<NodeJS.Timeout | null>(null)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    getOrderDetail(id)
      .then((res) => setOrder(res.order))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    return () => {
      if (paymentCheckTimer.current) {
        clearInterval(paymentCheckTimer.current)
      }
    }
  }, [])

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

  const handlePay = async () => {
    if (!id) return
    setPayLoading(true)
    try {
      const res = await createPayment(id)
      setQrCode(res.qr_code)
      setOutTradeNo(res.out_trade_no)
      setPayModalOpen(true)
      startPaymentCheck()
    } catch (err) {
      message.error((err as Error).message)
    } finally {
      setPayLoading(false)
    }
  }

  const startPaymentCheck = () => {
    if (paymentCheckTimer.current) {
      clearInterval(paymentCheckTimer.current)
    }
    paymentCheckTimer.current = setInterval(async () => {
      try {
        const res = await queryPayment(id!)
        if (res.status === OrderStatus.PAID || res.status === 2) {
          if (paymentCheckTimer.current) {
            clearInterval(paymentCheckTimer.current)
          }
          setPayModalOpen(false)
          message.success('支付成功！')
          const orderRes = await getOrderDetail(id!)
          setOrder(orderRes.order)
        }
      } catch (err) {
        // 忽略查询错误，继续轮询
      }
    }, 3000)
  }

  const handlePayModalClose = () => {
    if (paymentCheckTimer.current) {
      clearInterval(paymentCheckTimer.current)
    }
    setPayModalOpen(false)
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
            <Tag color={getOrderStatusColor(order.status)}>
              {getOrderStatusText(order.status)}
            </Tag>
          </div>
        }
        extra={
          order.status === OrderStatus.PENDING && (
            <Space>
              <Button
                type="primary"
                icon={<PayCircleOutlined />}
                loading={payLoading}
                onClick={handlePay}
              >
                去支付
              </Button>
              <Button danger onClick={handleCancel}>取消订单</Button>
            </Space>
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
          订单总计: <PriceDisplay cents={order.pay_amount} />
        </div>
      </Card>

      <Modal
        title="支付宝扫码支付"
        open={payModalOpen}
        onCancel={handlePayModalClose}
        footer={null}
        width={320}
        centered
      >
        <div className="flex flex-col items-center py-4">
          <QRCode value={qrCode} size={200} />
          <p className="mt-4 text-gray-500">请使用支付宝扫描二维码支付</p>
          <p className="text-sm text-gray-400">订单号: {outTradeNo}</p>
          <p className="text-sm text-orange-500 mt-2">二维码有效期 15 分钟</p>
        </div>
      </Modal>
    </div>
  )
}
