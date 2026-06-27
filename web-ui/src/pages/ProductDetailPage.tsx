import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  Button,
  Descriptions,
  Radio,
  InputNumber,
  Spin,
  Empty,
  message,
  Tag,
} from 'antd'
import { ShoppingCartOutlined } from '@ant-design/icons'
import { getProductDetail, getSkuList, createOrder } from '@/api/mall'
import { PriceDisplay } from '@/components/PriceDisplay'
import { formatDateTime, generateIdempotencyKey } from '@/utils/format'
import type { Product, Sku } from '@/types/api'

export default function ProductDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [product, setProduct] = useState<Product | null>(null)
  const [skus, setSkus] = useState<Sku[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedSku, setSelectedSku] = useState<string | null>(null)
  const [quantity, setQuantity] = useState(1)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    Promise.all([getProductDetail(id), getSkuList({ product_id: id, page: 1, page_size: 100 })])
      .then(([productRes, skuRes]) => {
        setProduct(productRes.product)
        setSkus(skuRes.list || [])
        if (skuRes.list?.length > 0) {
          setSelectedSku(skuRes.list[0].id)
        }
      })
      .finally(() => setLoading(false))
  }, [id])

  const selectedSkuInfo = skus.find((s) => s.id === selectedSku)

  const handleCreateOrder = async () => {
    if (!selectedSkuInfo) {
      message.warning('请选择 SKU')
      return
    }
    if (quantity <= 0) {
      message.warning('请输入购买数量')
      return
    }
    if (quantity > selectedSkuInfo.stock) {
      message.warning('库存不足')
      return
    }

    setSubmitting(true)
    try {
      const res = await createOrder({
        sku_id: selectedSkuInfo.id,
        quantity,
        remark: '',
        idempotency_key: generateIdempotencyKey(),
      })
      message.success('下单成功')
      navigate(`/orders/${res.order_id}`)
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

  if (!product) {
    return <Empty description="商品不存在" />
  }

  return (
    <div className="space-y-6">
      <Card>
        <div className="flex gap-8">
          <div className="w-80 h-80 bg-gray-100 flex items-center justify-center rounded-lg overflow-hidden">
            {product.main_image ? (
              <img src={product.main_image} alt={product.name} className="h-full w-full object-cover" />
            ) : (
              <span className="text-gray-400">暂无图片</span>
            )}
          </div>
          <div className="flex-1 space-y-6">
            <h1 className="text-2xl font-bold">{product.name}</h1>
            <Descriptions column={2}>
              <Descriptions.Item label="品牌">{product.brand || '-'}</Descriptions.Item>
              <Descriptions.Item label="状态">
                <Tag color={product.status === 1 ? 'green' : 'default'}>
                  {product.status === 1 ? '在售' : '下架'}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">{formatDateTime(product.created_at)}</Descriptions.Item>
            </Descriptions>
            <div className="text-gray-600">{product.detail || '暂无商品详情'}</div>
          </div>
        </div>
      </Card>

      <Card title="选择规格">
        {skus.length === 0 ? (
          <Empty description="暂无 SKU" />
        ) : (
          <div className="space-y-6">
            <div>
              <div className="mb-2 font-medium">SKU:</div>
              <Radio.Group
                value={selectedSku}
                onChange={(e) => setSelectedSku(e.target.value)}
                optionType="button"
                buttonStyle="solid"
              >
                {skus.map((sku) => (
                  <Radio.Button key={sku.id} value={sku.id} disabled={sku.status !== 1}>
                    {sku.name}
                  </Radio.Button>
                ))}
              </Radio.Group>
            </div>

            {selectedSkuInfo && (
              <div className="flex items-center gap-8">
                <div>
                  <div className="text-gray-500">价格</div>
                  <PriceDisplay cents={selectedSkuInfo.price} className="text-2xl" />
                </div>
                <div>
                  <div className="text-gray-500">库存</div>
                  <div className="text-lg">{selectedSkuInfo.stock}</div>
                </div>
                <div>
                  <div className="text-gray-500">数量</div>
                  <InputNumber
                    min={1}
                    max={selectedSkuInfo.stock}
                    value={quantity}
                    onChange={(v) => setQuantity(v || 1)}
                    size="large"
                  />
                </div>
              </div>
            )}

            <Button
              type="primary"
              size="large"
              icon={<ShoppingCartOutlined />}
              loading={submitting}
              onClick={handleCreateOrder}
            >
              立即下单
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}
