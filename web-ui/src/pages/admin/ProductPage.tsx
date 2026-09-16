import { useCallback, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Button, Card, Descriptions, Table, Tag } from 'antd'
import { adminProduct, adminSkus, deleteProduct, deleteSku } from '@/api/admin'
import { ProductEditor, SkuEditor } from '@/components/admin/CatalogEditors'
import { ConfirmAction } from '@/components/admin/AdminActions'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { formatPrice } from '@/utils/format'
import { formatSpecs, readPage } from '@/utils/mall'
import type { Sku } from '@/types/api'

export default function ProductPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const page = readPage(params.get('page'))
  const [edit, setEdit] = useState(false)
  const [skuEditor, setSkuEditor] = useState<Sku | 'new' | null>(null)
  const load = useCallback(async (signal: AbortSignal) => {
    const [detail, skus] = await Promise.all([adminProduct(id, signal), adminSkus({ product_id: id, page, page_size: 10, status: -1 }, signal)])
    if (!detail.product || detail.product.id !== id) throw new Error('商品不存在或响应不匹配')
    return { product: detail.product, skus }
  }, [id, page])
  const resource = useResource(load)
  const product = resource.data?.product
  const back = typeof location.state?.adminList === 'string' && /^\/admin\/products(?:\?|$)/.test(location.state.adminList) ? location.state.adminList : '/admin/products'
  return <><Link className="commerce-text-link" to={back}>← 返回商品管理</Link><ResourceState {...resource} retry={resource.reload} />{product && <><div className="commerce-heading"><div><h1>{product.name}</h1><p className="break-anywhere">{product.id}</p></div><div className="admin-actions"><Button onClick={resource.reload}>刷新详情</Button><Button onClick={() => setEdit(true)}>编辑商品</Button><ConfirmAction label="删除商品" danger description={`删除“${product.name}”后无法通过此页面恢复，请确认没有继续销售需求。`} action={(signal) => deleteProduct(id, signal)} onSaved={() => navigate(back, { replace: true })} /></div></div>
    <Card><Descriptions column={{ xs: 1, md: 2 }}><Descriptions.Item label="供应商">{product.providor || '—'}</Descriptions.Item><Descriptions.Item label="归属用户">{product.user_id || '—'}</Descriptions.Item><Descriptions.Item label="状态">{product.status === 1 ? '上架' : product.status === 0 ? '下架' : `未知(${product.status})`}</Descriptions.Item><Descriptions.Item label="推荐备注">{product.agent_comment || '—'}</Descriptions.Item></Descriptions><p className="admin-prewrap">{product.content || '暂无描述'}</p></Card>
    <div className="commerce-heading"><h2>商品规格</h2><Button type="primary" onClick={() => setSkuEditor('new')}>新增商城规格</Button></div><Table className="admin-table" rowKey="id" pagination={false} scroll={{ x: 780 }} dataSource={resource.data?.skus.list || []} columns={[
      { title: '规格', dataIndex: 'name' }, { title: '描述', dataIndex: 'specs', render: formatSpecs }, { title: '价格', dataIndex: 'price', render: formatPrice }, { title: '库存', dataIndex: 'stock' }, { title: '已售', dataIndex: 'sold' }, { title: '状态', dataIndex: 'status', render: (status) => <Tag>{status === 1 ? '上架' : status === 0 ? '下架' : `未知(${status})`}</Tag> },
      { title: '操作', render: (_, sku) => <div className="admin-actions"><Button onClick={() => setSkuEditor(sku)}>编辑规格</Button><ConfirmAction label="删除规格" danger description={`删除规格“${sku.name}”，此操作不可通过页面恢复。`} action={(signal) => deleteSku(sku.id, signal)} onSaved={resource.reload} /></div> },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data?.skus.total || 0} onChange={(value) => setParams({ page: String(value) })} />
    {edit && <ProductEditor key={id} product={product} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); resource.reload() }} />}{skuEditor && <SkuEditor key={skuEditor === 'new' ? 'new' : skuEditor.id} productId={id} sku={skuEditor === 'new' ? undefined : skuEditor} onClose={() => setSkuEditor(null)} onSaved={() => { setSkuEditor(null); resource.reload() }} />}</>}
  </>
}
