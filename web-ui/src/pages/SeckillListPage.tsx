import { useCallback } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { Button, Empty, Tag } from 'antd'
import { getActivityList } from '@/api/seckill'
import { Pagination } from '@/components/Pagination'
import { ProductImage } from '@/components/ProductImage'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { useNow } from '@/hooks/useNow'
import { readPage, readPageSize } from '@/utils/mall'
import { activityState, countdown, formatMilliseconds } from '@/utils/seckill'

export default function SeckillListPage() {
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const page = readPage(params.get('page'))
  const size = readPageSize(params.get('page_size'), [10, 20, 50])
  const load = useCallback((signal: AbortSignal) => getActivityList({ page, page_size: size }, signal), [page, size])
  const { data, loading, error, reload } = useResource(load)
  const now = useNow()
  return <div className="commerce-page">
    <div className="commerce-heading"><div><span className="eyebrow">好东西，也有好时机</span><h1>限时秒杀</h1><p>先看清活动，再决定是否参与。抢购结果以服务端确认为准。</p></div><Button onClick={reload} loading={loading}>刷新活动</Button></div>
    <ResourceState loading={loading} error={error} retry={reload} />
    {data && <>{data.list?.length ? <div className="seckill-grid">{data.list.map((activity) => {
      const state = activityState(activity, now)
      return <article className="seckill-card" key={activity.id}><ProductImage src={activity.banner_url} name={activity.title} /><div className="seckill-card-body"><Tag color={state.open ? 'green' : 'default'}>{state.label}</Tag><h2>{activity.title}</h2><p>{activity.description || '活动详情待补充'}</p><div className="seckill-times"><span>开始：{formatMilliseconds(activity.start_time)}</span><span>结束：{formatMilliseconds(activity.end_time)}</span></div>{state.deadline > 0 && <div className="seckill-countdown"><span>{state.open ? '距离结束' : '距离开始'}</span><strong>{countdown(state.deadline, now)}</strong></div>}<Link to={`/seckill/${encodeURIComponent(activity.id)}`} state={{ activityList: `${location.pathname}${location.search}` }}><Button type={state.open ? 'primary' : 'default'} block>查看活动</Button></Link></div></article>
    })}</div> : <Empty description="暂无秒杀活动">{page > 1 && <Button onClick={() => setParams({})}>回到第一页</Button>}</Empty>}<Pagination page={page} pageSize={size} total={data.total} onChange={(value, nextSize) => setParams({ page: String(size === nextSize ? value : 1), page_size: String(nextSize) })} /></>}
    <p className="commerce-note">倒计时使用设备时间，仅供参考。秒杀订单与商城订单分开查询。</p>
    <Link className="commerce-text-link" to="/seckill/orders">已有秒杀订单号？查询结果 →</Link>
  </div>
}
