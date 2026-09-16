import { Link } from 'react-router-dom'
import { PriceDisplay } from './PriceDisplay'
import { formatPrice } from '@/utils/format'
import type { OrderItem } from '@/types/api'

export function OrderItems({ items }: { items: OrderItem[] }) {
  if (!items?.length) return <p className="commerce-note">暂无商品明细</p>
  return <div className="order-items">{items.map((item, index) => <div className="order-item" key={`${item.sku_id}-${index}`}>
    <div><Link to={`/products/${encodeURIComponent(item.product_id)}?sku=${encodeURIComponent(item.sku_id)}`}>{item.sku_name || '商品规格'}</Link><p>单价 {formatPrice(item.price)} · 数量 {item.quantity}</p></div>
    <PriceDisplay cents={item.total_amount} />
  </div>)}</div>
}
