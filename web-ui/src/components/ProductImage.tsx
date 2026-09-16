import { useState } from 'react'
import { ShoppingOutlined } from '@ant-design/icons'

export function ProductImage({ src, name }: { src: string; name: string }) {
  const [failedSource, setFailedSource] = useState<string>()
  return <div className="product-image">
    {src && src !== failedSource
      ? <img src={src} alt={name} loading="lazy" onError={() => setFailedSource(src)} />
      : <div className="product-image-placeholder" role="img" aria-label={`${name}暂无图片`}><ShoppingOutlined aria-hidden /><span>商品图片待补充</span></div>}
  </div>
}
