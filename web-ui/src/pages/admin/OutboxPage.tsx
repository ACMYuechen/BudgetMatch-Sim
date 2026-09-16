import { useCallback } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Input, Select, Table, Tag } from 'antd'
import { outboxEvents, outboxStats } from '@/api/admin'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { readPage } from '@/utils/mall'
import { outboxStatuses, readFilter } from '@/utils/admin'
import { formatMilliseconds } from '@/utils/seckill'

export default function OutboxPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const page = readPage(params.get('page'))
  const status = readFilter(params.get('status'), [0, 1, 2, 3])
  const aggregate = (params.get('aggregate_id') || '').slice(0, 128)
  const eventType = (params.get('event_type') || '').slice(0, 128)
  const dedup = (params.get('dedup_key') || '').slice(0, 256)
  const load = useCallback((signal: AbortSignal) => outboxEvents({ page, page_size: 10, status, aggregate_id: aggregate, event_type: eventType, dedup_key: dedup }, signal), [page, status, aggregate, eventType, dedup])
  const resource = useResource(load)
  const stats = useResource(outboxStats)
  const counts = stats.data?.counts || []
  const update = (key: string, value: string) => { const next = new URLSearchParams(params); next.set(key, value); if (key !== 'page') next.delete('page'); setParams(next) }
  return <><div className="commerce-heading"><div><span className="eyebrow">可靠消息</span><h1>消息 Outbox</h1><p>检查订单事件投递情况。重放只重新排队，不代表已发送成功。</p></div><Button onClick={() => { resource.reload(); stats.reload() }}>刷新事件</Button></div>
    <ResourceState loading={stats.loading} error={stats.error} retry={stats.reload} />{stats.data && <><div className="outbox-stats">{Object.entries(outboxStatuses).map(([value, label]) => <div key={value}><span>{label}</span><strong>{counts.filter((item) => item.status === Number(value)).reduce((total, item) => total + item.count, 0)}</strong></div>)}</div><p className="commerce-note">全局统计，不受下方筛选影响。最早待发送时间：{formatMilliseconds(stats.data.oldest_pending_at)}</p></>}
    <div className="admin-filters"><Select aria-label="Outbox 状态筛选" value={status} onChange={(value) => update('status', String(value))} options={[{ value: -1, label: '全部投递状态' }, ...Object.entries(outboxStatuses).map(([value, label]) => ({ value: Number(value), label }))]} />{[['aggregate_id', '关联订单 ID', aggregate], ['event_type', '事件类型', eventType], ['dedup_key', '去重键', dedup]].map(([key, label, value]) => <Input.Search key={`${key}:${value}`} aria-label={label} defaultValue={value} maxLength={key === 'dedup_key' ? 256 : 128} placeholder={label} onSearch={(text) => update(key, text.trim())} />)}</div>
    <ResourceState {...resource} retry={resource.reload} />{resource.data && <><Table className="admin-table" rowKey="id" pagination={false} scroll={{ x: 800 }} dataSource={resource.data.list || []} columns={[
      { title: '事件 ID', dataIndex: 'id', render: (id) => <Link to={`/admin/outbox/${encodeURIComponent(id)}`} state={{ adminList: `${location.pathname}${location.search}` }}>{id}</Link> }, { title: '关联订单', dataIndex: 'aggregate_id' }, { title: '类型', dataIndex: 'event_type' }, { title: '状态', dataIndex: 'status', render: (value) => <Tag color={value === 3 ? 'red' : value === 2 ? 'green' : 'default'}>{outboxStatuses[value] || `未知(${value})`}</Tag> }, { title: '尝试次数', render: (_, item) => `${item.attempts} / ${item.max_attempts}` },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data.total} onChange={(value) => update('page', String(value))} /></>}
  </>
}
