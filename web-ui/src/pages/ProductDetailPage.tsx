import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Alert, Button, Card, Empty, Input, InputNumber, Modal, Radio, Tag } from 'antd'
import { getProductDetail, getAllProductSkus, createOrder } from '@/api/mall'
import { ProductImage } from '@/components/ProductImage'
import { PriceDisplay } from '@/components/PriceDisplay'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { formatPrice, generateIdempotencyKey } from '@/utils/format'
import { formatSpecs } from '@/utils/mall'
import type { Product, Sku } from '@/types/api'

function PurchasePanel({ product, skus }: { product: Product; skus: Sku[] }) {
  const [params] = useSearchParams()
  const { state } = useLocation()
  const navigate = useNavigate()
  const available = (sku: Sku) => sku.status === 1 && sku.stock > 0
  const requestedSku = params.get('sku')
  const [selectedId, setSelectedId] = useState(() => requestedSku
    ? skus.find((sku) => sku.id === requestedSku && available(sku))?.id : skus.find(available)?.id)
  const [quantity, setQuantity] = useState<number | null>(1)
  const [remark, setRemark] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const requestRef = useRef<AbortController | null>(null)
  const keys = useRef(new Map<string, string>())
  useEffect(() => () => requestRef.current?.abort(), [])
  const selected = skus.find((sku) => sku.id === selectedId)
  const requestedUnavailable = requestedSku && !skus.some((sku) => sku.id === requestedSku && available(sku))
  const priorPrice = state?.recommendedSkuId === selected?.id && Number.isSafeInteger(state?.recommendationPriceCents) ? state.recommendationPriceCents as number : undefined
  const maxQuantity = Math.min(selected?.stock || 0, 99)
  const total = (selected?.price || 0) * (quantity || 0)
  const valid = product.status === 1 && selected && available(selected) && quantity !== null && Number.isInteger(quantity) && quantity >= 1 && quantity <= maxQuantity && Number.isSafeInteger(total)

  const submit = async () => {
    if (!valid || !selected || quantity === null || requestRef.current) return
    const controller = new AbortController()
    requestRef.current = controller
    setSubmitting(true)
    setError('')
    // 同一购买内容重试时复用幂等键，避免超时后再次创建订单。
    const signature = JSON.stringify([selected.id, quantity, remark.trim()])
    const key = keys.current.get(signature) || generateIdempotencyKey()
    keys.current.set(signature, key)
    try {
      const result = await createOrder({ sku_id: selected.id, quantity, remark: remark.trim(), idempotency_key: key }, controller.signal)
      if (!result.order_id) throw new Error('服务未返回订单号，请先到我的订单确认结果')
      if (!controller.signal.aborted) navigate(`/orders/${encodeURIComponent(result.order_id)}`)
    } catch (err) {
      if (!controller.signal.aborted) setError((err as Error).message)
    } finally {
      if (!controller.signal.aborted) { requestRef.current = null; setSubmitting(false) }
    }
  }

  return <Card title="选一个适合你的规格" className="purchase-panel">
    {product.status !== 1 && <Alert type="warning" showIcon message="商品已下架，暂时无法购买" />}
    {requestedUnavailable && <Alert type="warning" showIcon message="指定规格已下架或缺货" description="没有为你自动切换其他规格，请核对后自行选择。" />}
    {selected && priorPrice !== undefined && priorPrice !== selected.price && <Alert type="info" showIcon message="价格已更新" description={`推荐时为 ${formatPrice(priorPrice)}，当前为 ${formatPrice(selected.price)}。下单前请确认最新金额。`} />}
    {!skus.length ? <Empty description="暂时没有可购买的规格" /> : <>
      <Radio.Group aria-label="商品规格" className="sku-options" value={selectedId} disabled={submitting || product.status !== 1} onChange={(event) => { setSelectedId(event.target.value); setQuantity(1) }}>
        {skus.map((sku) => <Radio key={sku.id} value={sku.id} disabled={!available(sku)} className="sku-option">
          <span className="sku-option-name">{sku.name} <span>{formatPrice(sku.price)}</span></span>
          <span className="sku-specs">{formatSpecs(sku.specs)}{sku.status !== 1 ? ' · 已下架' : sku.stock <= 0 ? ' · 暂时缺货' : ''}</span>
        </Radio>)}
      </Radio.Group>
      {selected ? <>
        <div className="purchase-quantity"><div><label htmlFor="purchase-quantity">购买数量</label><span>库存 {selected.stock} 件 · 每单最多 {maxQuantity} 件</span></div>
          <InputNumber id="purchase-quantity" value={quantity} min={1} max={maxQuantity} precision={0} disabled={submitting || product.status !== 1} onChange={setQuantity} />
        </div>
        <label className="purchase-remark">订单备注（选填）<Input.TextArea value={remark} maxLength={200} showCount autoSize={{ minRows: 2, maxRows: 4 }} disabled={submitting} onChange={(event) => setRemark(event.target.value)} placeholder="有什么需要告诉我们的吗？" /></label>
        <div className="purchase-total"><span>商品合计</span><PriceDisplay cents={total} /></div>
        <Button type="primary" size="large" block disabled={!valid} onClick={() => { setConfirmOpen(true); setError('') }}>确认购买</Button>
        <p className="commerce-note">确认后创建待支付订单，实际成交信息以订单为准。</p>
      </> : <Alert type="info" showIcon message={skus.some(available) ? '请选择一个可购买的规格' : '所有规格暂时缺货，请稍后再来看看'} />}
    </>}
    <Modal title="确认订单" open={confirmOpen} okText={error ? '重试提交' : '提交订单'} cancelText="再想想" confirmLoading={submitting} okButtonProps={{ disabled: !valid }} cancelButtonProps={{ disabled: submitting }} closable={!submitting} maskClosable={!submitting} keyboard={!submitting} onCancel={() => setConfirmOpen(false)} onOk={submit}>
      <div className="checkout-summary"><h3>{product.name}</h3><p>{selected?.name} · {quantity} 件</p><p>{selected && formatSpecs(selected.specs)}</p>{remark.trim() && <p>备注：{remark.trim()}</p>}<div className="purchase-total"><span>应付金额</span><PriceDisplay cents={total} /></div></div>
      {error && <Alert type="error" showIcon message={error} description={<span>若提交结果不确定，请先<Link to="/orders">查看我的订单</Link>。在本页重试同一购买内容会复用请求标识。</span>} />}
    </Modal>
  </Card>
}

export default function ProductDetailPage() {
  const { id = '' } = useParams()
  const { state } = useLocation()
  const [params] = useSearchParams()
  const recommendationTarget = typeof state?.recommendationReturnTo === 'string' && /^\/recommend(?:\/[^/?#]+)?(?:\?[^#]*)?$/.test(state.recommendationReturnTo) ? state.recommendationReturnTo : null
  const listTarget = typeof state?.productList === 'string' && /^\/products(?:\?|$)/.test(state.productList) ? state.productList : '/products'
  const load = useCallback(async (signal: AbortSignal) => {
    const [result, skus] = await Promise.all([getProductDetail(id, signal), getAllProductSkus(id, signal)])
    return { product: result.product, skus }
  }, [id])
  const { data, loading, error, reload } = useResource(load)
  return <div className="commerce-page">
    <Link className="commerce-text-link" to={listTarget}>← 返回商品列表</Link>
    {recommendationTarget && <Link className="commerce-text-link" to={recommendationTarget}>← 返回预算推荐</Link>}
    <ResourceState loading={loading} error={error} retry={reload} />
    {data?.product ? <div className="product-detail-grid">
      <div className="product-story"><ProductImage src={data.product.image} name={data.product.name} /><div className="product-description">
        <span className="eyebrow">{data.product.providor || '来源未标注'}</span><h1>{data.product.name}</h1>
        <Tag color={data.product.status === 1 ? 'green' : 'default'}>{data.product.status === 1 ? '在售' : '已下架'}</Tag>
        <h2>关于这件商品</h2><p>{data.product.content || '商品详情待补充。'}</p>
      </div></div>
      <PurchasePanel key={`${id}:${params.get('sku') || ''}`} product={data.product} skus={data.skus} />
    </div> : data && <Empty description="商品不存在或已被移除" />}
  </div>
}
