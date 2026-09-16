import { useCallback, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Alert, Button, Card, Descriptions, Table, Tag } from 'antd'
import { activityAction, adminActivity, adminSeckillSkus, deleteSeckillSku } from '@/api/admin'
import { ActivityEditor, SeckillSkuEditor } from '@/components/admin/CatalogEditors'
import { ConfirmAction } from '@/components/admin/AdminActions'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { formatPrice } from '@/utils/format'
import { readPage } from '@/utils/mall'
import { activityStatuses } from '@/utils/admin'
import { availableStock, formatMilliseconds } from '@/utils/seckill'
import type { SeckillSku } from '@/types/api'

export default function ActivityPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const page = readPage(params.get('page'))
  const [edit, setEdit] = useState(false)
  const [skuEditor, setSkuEditor] = useState<SeckillSku | 'new' | null>(null)
  const load = useCallback(async (signal: AbortSignal) => {
    const [detail, skus] = await Promise.all([adminActivity(id, signal), adminSeckillSkus({ activity_id: id, page, page_size: 10 }, signal)])
    if (!detail.activity || detail.activity.id !== id) throw new Error('活动不存在或响应不匹配')
    return { activity: detail.activity, skus }
  }, [id, page])
  const resource = useResource(load)
  const activity = resource.data?.activity
  const editable = activity?.status === 0
  const back = typeof location.state?.adminList === 'string' && /^\/admin\/activities(?:\?|$)/.test(location.state.adminList) ? location.state.adminList : '/admin/activities'
  return <><Link className="commerce-text-link" to={back}>← 返回活动管理</Link><ResourceState {...resource} retry={resource.reload} />{activity && <><div className="commerce-heading"><div><h1>{activity.title}</h1><Tag>{activityStatuses[activity.status] || `未知(${activity.status})`}</Tag></div><div className="admin-actions"><Button onClick={resource.reload}>刷新详情</Button><Button disabled={!editable} onClick={() => setEdit(true)}>编辑活动</Button><ConfirmAction label="删除活动" danger disabled={!editable} description={`删除活动“${activity.title}”。请先确认没有仍在排队的请求或需要查询的订单；此操作不可通过页面恢复。`} action={(signal) => activityAction(id, 'delete', signal)} onSaved={() => navigate(back, { replace: true })} /></div></div>
    <Card><Descriptions column={{ xs: 1, md: 2 }}><Descriptions.Item label="开始时间">{formatMilliseconds(activity.start_time)}</Descriptions.Item><Descriptions.Item label="结束时间">{formatMilliseconds(activity.end_time)}</Descriptions.Item></Descriptions><p className="admin-prewrap">{activity.description || '暂无描述'}</p><div className="admin-actions">
      <ConfirmAction label="预热活动" disabled={!editable} description="预热会按总库存减已售重置 Redis 库存，并将活动改为预热状态（时间范围内可接受抢购）。请先确认没有仍在排队的请求，不要重复预热。" action={(signal) => activityAction(id, 'preheat', signal)} onSaved={resource.reload} />
      <ConfirmAction label="上线活动" disabled={![0, 2].includes(activity.status)} description="上线前请确认商品、时间和库存预热已完成。达到开始时间后用户可以参与抢购。" action={(signal) => activityAction(id, 'online', signal)} onSaved={resource.reload} />
      <ConfirmAction label="下线活动" danger disabled={![1, 2].includes(activity.status)} description="下线后停止新的参与请求；已经受理的排队请求不因此撤销。" action={(signal) => activityAction(id, 'offline', signal)} onSaved={resource.reload} />
    </div></Card>
    {!editable && <Alert type="info" message="活动未下线，已禁用配置与商品修改。请先下线并核对排队请求。" />}
    <div className="commerce-heading"><h2>秒杀商品</h2><Button type="primary" disabled={!editable} onClick={() => setSkuEditor('new')}>新增秒杀商品</Button></div><Table className="admin-table" rowKey="id" pagination={false} scroll={{ x: 820 }} dataSource={resource.data?.skus.list || []} columns={[
      { title: '标题', dataIndex: 'title' }, { title: '秒杀价', dataIndex: 'seckill_price', render: formatPrice }, { title: '总库存', dataIndex: 'stock' }, { title: '已售', dataIndex: 'sold' }, { title: '参考剩余', render: (_, sku) => availableStock(sku) }, { title: '状态', dataIndex: 'status', render: (value) => value === 1 ? '上架' : value === 0 ? '下架' : `禁用(${value})` },
      { title: '操作', render: (_, sku) => <div className="admin-actions"><Button disabled={!editable} onClick={() => setSkuEditor(sku)}>编辑秒杀商品</Button><ConfirmAction label="删除秒杀商品" danger disabled={!editable} description={`删除“${sku.title}”，请先核对库存与已受理订单。`} action={(signal) => deleteSeckillSku(sku.id, signal)} onSaved={resource.reload} /></div> },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data?.skus.total || 0} onChange={(value) => setParams({ page: String(value) })} />
    {edit && editable && <ActivityEditor key={id} activity={activity} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); resource.reload() }} />}{skuEditor && editable && <SeckillSkuEditor key={skuEditor === 'new' ? 'new' : skuEditor.id} activityId={id} sku={skuEditor === 'new' ? undefined : skuEditor} onClose={() => setSkuEditor(null)} onSaved={() => { setSkuEditor(null); resource.reload() }} />}</>}
  </>
}
