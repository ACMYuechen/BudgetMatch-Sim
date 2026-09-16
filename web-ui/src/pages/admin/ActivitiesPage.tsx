import { useCallback, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Alert, Button, Select, Table, Tag } from 'antd'
import { adminActivities } from '@/api/admin'
import { ActivityEditor } from '@/components/admin/CatalogEditors'
import { ResourceState } from '@/components/ResourceState'
import { Pagination } from '@/components/Pagination'
import { useResource } from '@/hooks/useResource'
import { readPage } from '@/utils/mall'
import { activityStatuses, readFilter } from '@/utils/admin'
import { formatMilliseconds } from '@/utils/seckill'

export default function ActivitiesPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const page = readPage(params.get('page'))
  const status = readFilter(params.get('status'), [1, 2], 0)
  const [create, setCreate] = useState(false)
  const load = useCallback((signal: AbortSignal) => adminActivities({ page, page_size: 10, status }, signal), [page, status])
  const resource = useResource(load)
  return <><div className="commerce-heading"><div><span className="eyebrow">活动运营</span><h1>秒杀活动</h1></div><div className="admin-actions"><Button onClick={resource.reload}>刷新活动</Button><Button type="primary" onClick={() => setCreate(true)}>新建活动</Button></div></div>
    <div className="admin-filters"><Select aria-label="活动状态筛选" value={status} onChange={(value) => setParams({ status: String(value) })} options={[{ value: 0, label: '全部活动' }, { value: 1, label: '已上线' }, { value: 2, label: '已预热' }]} /></div>
    <Alert type="info" message="活动状态与时间分开管理。已上线不代表已经开始；配置修改请先下线，再根据库存情况预热。" />
    <ResourceState {...resource} retry={resource.reload} />{resource.data && <><Table className="admin-table" rowKey="id" dataSource={resource.data.list || []} pagination={false} scroll={{ x: 700 }} columns={[
      { title: '活动', dataIndex: 'title', render: (title, item) => <Link to={`/admin/activities/${encodeURIComponent(item.id)}`} state={{ adminList: `${location.pathname}${location.search}` }}>{title}</Link> }, { title: '开始时间', dataIndex: 'start_time', render: formatMilliseconds }, { title: '结束时间', dataIndex: 'end_time', render: formatMilliseconds }, { title: '状态', dataIndex: 'status', render: (value) => <Tag>{activityStatuses[value] || `未知(${value})`}</Tag> },
    ]} /><Pagination page={page} pageSize={10} pageSizeOptions={[10]} total={resource.data.total} onChange={(value) => setParams({ page: String(value), status: String(status) })} /></>}
    {create && <ActivityEditor onClose={() => setCreate(false)} onSaved={() => { setCreate(false); resource.reload() }} />}
  </>
}
