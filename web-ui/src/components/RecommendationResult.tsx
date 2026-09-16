import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { Alert, Button, Tag } from 'antd'
import { ArrowRightOutlined } from '@ant-design/icons'
import { getSkuDetail } from '@/api/mall'
import { formatPrice } from '@/utils/format'
import { isRecommendation, recommendationError } from '@/utils/recommendation'
import type { AgentBundleItem, AgentRecommendResp } from '@/types/api'

function RecommendedItem({ item }: { item: AgentBundleItem }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const requestRef = useRef<AbortController | null>(null)
  useEffect(() => () => requestRef.current?.abort(), [])
  const mallItem = item.source === 'mall' || item.source === 'mall+rag'

  const viewProduct = async () => {
    if (!mallItem || requestRef.current) return
    const controller = new AbortController()
    requestRef.current = controller
    setLoading(true)
    setError('')
    try {
      const { sku } = await getSkuDetail(item.id, controller.signal)
      if (sku?.id !== item.id || !sku.product_id) throw new Error('无法定位这个规格，请稍后重试')
      if (!controller.signal.aborted) navigate(`/products/${encodeURIComponent(sku.product_id)}?sku=${encodeURIComponent(sku.id)}`, {
        state: { recommendationReturnTo: `${location.pathname}${location.search}`, recommendedSkuId: sku.id, recommendationPriceCents: item.price_cents },
      })
    } catch (err) {
      if (!controller.signal.aborted) setError(recommendationError(err))
    } finally {
      if (!controller.signal.aborted) { requestRef.current = null; setLoading(false) }
    }
  }

  return <article className="recommend-product">
    <div className="recommend-product-heading"><h3>{item.name}</h3><strong>{formatPrice(item.price_cents)}</strong></div>
    <div className="recommend-product-tags"><Tag>{mallItem ? '商城商品' : item.source === 'mock' ? '演示商品' : '其他来源'}</Tag>{item.category && <Tag>{item.category}</Tag>}<span>{item.stock > 0 ? `推荐时库存 ${item.stock}` : '推荐时暂时缺货'}</span></div>
    {item.reason && <p>{item.reason}</p>}
    {error && <Alert type="error" showIcon message="商品信息读取失败" description={error} />}
    {mallItem ? <Button loading={loading} onClick={viewProduct} icon={<ArrowRightOutlined aria-hidden />}>{error ? '重试查看商品' : '查看并购买'}</Button>
      : <div className="recommend-unavailable"><span>{item.source === 'mock' ? '示例数据，不能直接购买' : '尚未关联商城规格'}</span><Link to={`/products?${new URLSearchParams({ keyword: item.name.slice(0, 100) })}`}>去商城搜索 →</Link></div>}
  </article>
}

export function RecommendationResult({ result }: { result: AgentRecommendResp }) {
  if (!isRecommendation(result)) return <Alert type="warning" showIcon message="这条历史推荐数据不完整" description="可继续描述需求，生成新的方案。" />
  const budget = result.intent.budget_cents
  const remainder = budget - result.total_price_cents
  const sum = result.items.reduce((total, item) => total + item.price_cents, 0)
  return <div className="recommend-result">
    <p className="recommend-summary">{result.summary || '已为你整理这份购物方案。'}</p>
    <div className="recommend-budget" aria-label="方案预算概览">
      <div><span>本轮预算</span><strong>{budget > 0 ? formatPrice(budget) : '未设定'}</strong></div>
      <div><span>方案合计</span><strong>{formatPrice(result.total_price_cents)}</strong></div>
      <div className={budget > 0 && remainder < 0 ? 'over-budget' : ''}><span>{remainder < 0 && budget > 0 ? '超出预算' : '预算剩余'}</span><strong>{budget > 0 ? formatPrice(Math.abs(remainder)) : '—'}</strong></div>
    </div>
    {budget > 0 && remainder < 0 && <Alert type="warning" showIcon message="这份方案超出了预算，可继续追问调整。" />}
    {sum !== result.total_price_cents && <Alert type="warning" showIcon message="商品明细与方案合计不一致" description="请查看商品当前价格，最终金额以订单确认页为准。" />}
    <div className="recommend-constraints">{result.intent.max_items > 0 && <Tag>最多 {result.intent.max_items} 件</Tag>}{result.intent.keywords?.map((text, index) => <Tag key={`k-${index}`}>{text}</Tag>)}{result.intent.preferences?.map((text, index) => <Tag color="green" key={`p-${index}`}>{text}</Tag>)}</div>
    {result.items.length ? <div className="recommend-products">{result.items.map((item, index) => <RecommendedItem key={`${item.id}-${index}`} item={item} />)}</div>
      : <div className="recommend-empty-result">暂时没有找到合适的商品，试试调整预算或换个需求描述。</div>}
    <p className="commerce-note">共 {result.items.length} 件，每种商品按 1 件计算。价格和库存为推荐时快照，购买前请核对最新信息；不同商品需分别下单。</p>
    {!!result.tools_used?.length && <details className="recommend-tools"><summary>查看检索记录</summary><ul>{result.tools_used.map((tool, index) => <li key={index}><Tag color={tool.success ? 'green' : 'orange'}>{tool.success ? '已完成' : '未完成'}</Tag><span>{tool.name}</span>{tool.detail && <p>{tool.detail}</p>}</li>)}</ul></details>}
  </div>
}
