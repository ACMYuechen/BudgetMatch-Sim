import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Empty, Input } from 'antd'
import { getProductList } from '@/api/mall'
import { ProductCard } from '@/components/ProductCard'
import { Pagination } from '@/components/Pagination'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { readPage, readPageSize } from '@/utils/mall'

export default function ProductListPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const keyword = (params.get('keyword') || '').trim().slice(0, 100)
  const page = readPage(params.get('page'))
  const pageSize = readPageSize(params.get('page_size'), [12, 24, 48])
  const [search, setSearch] = useState(keyword)
  useEffect(() => setSearch(keyword), [keyword])
  const load = useCallback((signal: AbortSignal) => getProductList({ page, page_size: pageSize, keyword, status: 1 }, signal), [page, pageSize, keyword])
  const { data, loading, error, reload } = useResource(load)

  const updateQuery = (nextPage: number, nextSize = pageSize, nextKeyword = keyword) => {
    const next = new URLSearchParams()
    if (nextKeyword.trim()) next.set('keyword', nextKeyword.trim())
    if (nextPage > 1) next.set('page', String(nextPage))
    if (nextSize !== 12) next.set('page_size', String(nextSize))
    setParams(next)
  }

  return <div className="commerce-page">
    <div className="commerce-heading"><div><span className="eyebrow">每一种需要，都有合适的选择</span><h1>发现商品</h1><p>先逛逛，再把喜欢的放进预算里。</p></div><Link className="commerce-text-link" to="/recommend">让预算助手帮我选 →</Link></div>
    <form className="catalog-toolbar" onSubmit={(event) => { event.preventDefault(); updateQuery(1, pageSize, search) }}>
      <Input aria-label="搜索商品" placeholder="输入商品名称或关键词" value={search} maxLength={100} allowClear onChange={(event) => setSearch(event.target.value)} size="large" />
      <Button type="primary" size="large" htmlType="submit">搜索</Button>
      {keyword && <Button size="large" onClick={() => { setSearch(''); updateQuery(1, pageSize, '') }}>清除筛选</Button>}
    </form>
    <ResourceState loading={loading} error={error} retry={reload} />
    {data && <>
      <p className="list-summary" role="status">{keyword ? `“${keyword}”的搜索结果` : '在售商品'} · 共 {data.total} 件</p>
      {data.list?.length ? <div className="catalog-grid">{data.list.map((product) => <ProductCard key={product.id} product={product} returnTo={`${location.pathname}${location.search}`} />)}</div>
        : <Empty description={page > 1 ? '这一页没有商品了' : keyword ? '没有找到匹配的商品，试试其他关键词' : '暂时还没有在售商品'}>
          {page > 1 && <Button onClick={() => updateQuery(1)}>回到第一页</Button>}
        </Empty>}
      <Pagination page={page} pageSize={pageSize} total={data.total} pageSizeOptions={[12, 24, 48]} onChange={(p, size) => updateQuery(size === pageSize ? p : 1, size)} />
    </>}
  </div>
}
