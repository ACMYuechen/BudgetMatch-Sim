import { Link } from 'react-router-dom'
import { ArrowRightOutlined } from '@ant-design/icons'
import { ProductImage } from './ProductImage'
import type { Product } from '@/types/api'

export function ProductCard({ product, returnTo }: { product: Product; returnTo: string }) {
  return <Link className="catalog-card" to={`/products/${encodeURIComponent(product.id)}`} state={{ productList: returnTo }}>
    <ProductImage src={product.image} name={product.name} />
    <div className="catalog-card-body">
      <span className="catalog-provider">{product.providor || '来源未标注'}</span>
      <h2>{product.name}</h2>
      <p>{product.content || '了解商品详情，找到适合你的选择。'}</p>
      <span className="catalog-card-action">查看规格与价格 <ArrowRightOutlined aria-hidden /></span>
    </div>
  </Link>
}
