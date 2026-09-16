import { useCallback, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Input, Select, Table, Tag } from 'antd'
import { adminProducts } from '@/api/admin'
import { ProductEditor } from '@/components/admin/CatalogEditors'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { readPage } from '@/utils/mall'
import { readFilter, statusOptions } from '@/utils/admin'

export default function ProductsPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const page = readPage(params.get('page'))
  const keyword = (params.get('keyword') || '').slice(0, 100)
  const status = readFilter(params.get('status'), [0, 1])
  const [create, setCreate] = useState(false)
  const load = useCallback((signal: AbortSignal) => adminProducts({ page, page_size: 10, keyword, status }, signal), [page, keyword, status])
  const resource = useResource(load)
  const update = (key: string, value: string) => { const next = new URLSearchParams(params); next.set(key, value); if (key !== 'page') next.delete('page'); setParams(next) }
  return <><div className="commerce-heading"><div><span className="eyebrow">商品目录</span><h1>商品与规格</h1></div><div className="admin-actions"><Button onClick={resource.reload}>刷新商品</Button><Button type="primary" onClick={() => setCreate(true)}>新建商品</Button></div></div>
    <div className="admin-filters"><Input.Search key={keyword} aria-label="管理商品搜索" defaultValue={keyword} maxLength={100} placeholder="搜索商品名称" onSearch={(value) => update('keyword', value.trim())} enterButton="搜索" /><Select aria-label="商品状态筛选" value={status} options={statusOptions} onChange={(value) => update('status', String(value))} /></div>
    <ResourceState {...resource} retry={resource.reload} />{resource.data && <><Table className="admin-table" rowKey="id" dataSource={resource.data.list || []} pagination={false} scroll={{ x: 700 }} columns={[
      { title: '商品', dataIndex: 'name', render: (name, item) => <Link to={`/admin/products/${encodeURIComponent(item.id)}`} state={{ adminList: `${location.pathname}${location.search}` }}>{name}</Link> },
      { title: '供应商', dataIndex: 'providor' }, { title: '归属用户', dataIndex: 'user_id' }, { title: '状态', dataIndex: 'status', render: (value) => <Tag color={value === 1 ? 'green' : 'default'}>{value === 1 ? '上架' : value === 0 ? '下架' : `未知(${value})`}</Tag> },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data.total} onChange={(value) => update('page', String(value))} /></>}
    {create && <ProductEditor onClose={() => setCreate(false)} onSaved={() => { setCreate(false); resource.reload() }} />}
  </>
}
