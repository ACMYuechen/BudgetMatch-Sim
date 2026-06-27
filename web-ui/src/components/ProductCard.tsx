import { Card, Tag } from 'antd'
import type { Product } from '@/types/api'

interface ProductCardProps {
  product: Product
  onClick?: (product: Product) => void
}

export function ProductCard({ product, onClick }: ProductCardProps) {
  return (
    <Card
      hoverable
      cover={
        <div className="h-48 bg-gray-100 flex items-center justify-center overflow-hidden">
          {product.main_image ? (
            <img alt={product.name} src={product.main_image} className="h-full w-full object-cover" />
          ) : (
            <span className="text-gray-400">暂无图片</span>
          )}
        </div>
      }
      onClick={() => onClick?.(product)}
      className="cursor-pointer"
    >
      <Card.Meta
        title={product.name}
        description={
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Tag color="blue">{product.brand || '默认品牌'}</Tag>
              <Tag color={product.status === 1 ? 'green' : 'default'}>
                {product.status === 1 ? '在售' : '下架'}
              </Tag>
            </div>
            <div className="text-gray-500 text-sm line-clamp-2">{product.detail || '暂无描述'}</div>
          </div>
        }
      />
    </Card>
  )
}
