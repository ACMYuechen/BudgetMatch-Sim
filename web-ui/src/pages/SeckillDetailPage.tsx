import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  Button,
  Radio,
  InputNumber,
  Spin,
  Empty,
  message,
  Tag,
  Descriptions,
} from 'antd'
import { ThunderboltOutlined } from '@ant-design/icons'
import { getActivityDetail, getSeckillSkuList, acquireToken, submitSeckillOrder } from '@/api/seckill'
import { PriceDisplay } from '@/components/PriceDisplay'
import { formatDateTime } from '@/utils/format'
import type { Activity, SeckillSku } from '@/types/api'

export default function SeckillDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [activity, setActivity] = useState<Activity | null>(null)
  const [skus, setSkus] = useState<SeckillSku[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedSku, setSelectedSku] = useState<string | null>(null)
  const [quantity, setQuantity] = useState(1)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    Promise.all([
      getActivityDetail(id),
      getSeckillSkuList({ activity_id: id, page: 1, page_size: 100 }),
    ])
      .then(([activityRes, skuRes]) => {
        setActivity(activityRes.activity)
        setSkus(skuRes.list || [])
        if (skuRes.list?.length > 0) {
          setSelectedSku(skuRes.list[0].id)
        }
      })
      .finally(() => setLoading(false))
  }, [id])

  const selectedSkuInfo = skus.find((s) => s.id === selectedSku)

  const handleSubmit = async () => {
    if (!id || !selectedSkuInfo) {
      message.warning('请选择 SKU')
      return
    }
    if (quantity <= 0) {
      message.warning('请输入购买数量')
      return
    }
    if (quantity > selectedSkuInfo.limit) {
      message.warning(`每人限购 ${selectedSkuInfo.limit} 件`)
      return
    }
    if (quantity > selectedSkuInfo.stock) {
      message.warning('库存不足')
      return
    }

    setSubmitting(true)
    try {
      const tokenRes = await acquireToken({ activity_id: id, sku_id: selectedSkuInfo.id })
      const orderRes = await submitSeckillOrder({
        activity_id: id,
        sku_id: selectedSkuInfo.id,
        quantity,
        token: tokenRes.token,
      })
      message.success('秒杀下单成功')
      navigate(`/orders/${orderRes.order_id}`)
    } catch (err) {
      message.error((err as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <Spin size="large" />
      </div>
    )
  }

  if (!activity) {
    return <Empty description="活动不存在" />
  }

  const now = new Date().getTime()
  const start = new Date(activity.start_time).getTime()
  const end = new Date(activity.end_time).getTime()
  const isOngoing = now >= start && now <= end

  return (
    <div className="space-y-6">
      <Card
        title={
          <div className="flex items-center gap-2">
            <ThunderboltOutlined className="text-red-500" />
            {activity.name}
          </div>
        }
      >
        <Descriptions column={2}>
          <Descriptions.Item label="状态">
            {isOngoing ? <Tag color="red">进行中</Tag> : now < start ? <Tag color="blue">即将开始</Tag> : <Tag>已结束</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatDateTime(activity.start_time)}</Descriptions.Item>
          <Descriptions.Item label="结束时间">{formatDateTime(activity.end_time)}</Descriptions.Item>
        </Descriptions>
        <p className="text-gray-600 mt-4">{activity.description || '暂无活动描述'}</p>
      </Card>

      <Card title="选择秒杀商品">
        {skus.length === 0 ? (
          <Empty description="暂无秒杀 SKU" />
        ) : (
          <div className="space-y-6">
            <Radio.Group
              value={selectedSku}
              onChange={(e) => setSelectedSku(e.target.value)}
              optionType="button"
              buttonStyle="solid"
            >
              {skus.map((sku) => (
                <Radio.Button key={sku.id} value={sku.id} disabled={sku.status !== 1}>
                  {sku.sku_name}
                </Radio.Button>
              ))}
            </Radio.Group>

            {selectedSkuInfo && (
              <div className="flex items-center gap-8">
                <div>
                  <div className="text-gray-500">秒杀价</div>
                  <PriceDisplay cents={selectedSkuInfo.seckill_price} className="text-2xl" />
                </div>
                <div>
                  <div className="text-gray-500">库存</div>
                  <div className="text-lg">{selectedSkuInfo.stock}</div>
                </div>
                <div>
                  <div className="text-gray-500">限购</div>
                  <div className="text-lg">{selectedSkuInfo.limit}</div>
                </div>
                <div>
                  <div className="text-gray-500">数量</div>
                  <InputNumber
                    min={1}
                    max={Math.min(selectedSkuInfo.limit, selectedSkuInfo.stock)}
                    value={quantity}
                    onChange={(v) => setQuantity(v || 1)}
                    size="large"
                  />
                </div>
              </div>
            )}

            <Button
              type="primary"
              danger
              size="large"
              icon={<ThunderboltOutlined />}
              loading={submitting}
              disabled={!isOngoing}
              onClick={handleSubmit}
            >
              {isOngoing ? '立即秒杀' : '活动未开始'}
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}
