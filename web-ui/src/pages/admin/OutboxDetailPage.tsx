import { useCallback } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { Alert, Button, Card, Descriptions, Tag } from 'antd'
import { outboxEvent, replayOutbox } from '@/api/admin'
import { ConfirmAction } from '@/components/admin/AdminActions'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { formatDateTime } from '@/utils/format'
import { formatMilliseconds } from '@/utils/seckill'
import { outboxStatuses } from '@/utils/admin'

function payloadText(value: string) { try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value } }
export default function OutboxDetailPage() {
  const { id = '' } = useParams()
  const { state } = useLocation()
  const back = typeof state?.adminList === 'string' && /^\/admin\/outbox(?:\?|$)/.test(state.adminList) ? state.adminList : '/admin/outbox'
  const load = useCallback(async (signal: AbortSignal) => {
    const data = await outboxEvent(id, signal)
    if (!data.event || data.event.id !== id) throw new Error('事件不存在或响应不匹配')
    return data.event
  }, [id])
  const resource = useResource(load)
  const event = resource.data
  return <><Link className="commerce-text-link" to={back}>← 返回消息列表</Link><ResourceState {...resource} retry={resource.reload} />{event && <><div className="commerce-heading"><div><h1>事件详情</h1><p className="break-anywhere">{event.id}</p><Tag>{outboxStatuses[event.status] || `未知(${event.status})`}</Tag></div><div className="admin-actions"><Button onClick={resource.reload}>刷新事件</Button>{event.status === 3 && <ConfirmAction label="重放死信" danger description="确认依赖故障已恢复后再重放。此操作将死信重新置为待发送，后续由后台投递器处理，可能再次触发下游消费；请核对去重键。" action={(signal) => replayOutbox(id, signal)} onSaved={resource.reload} />}</div></div>
    {event.last_error && <Alert type="warning" showIcon message="最近投递错误" description={<span className="admin-prewrap">{event.last_error}</span>} />}
    <Card><Descriptions column={{ xs: 1, md: 2 }}><Descriptions.Item label="关联订单"><Link to={`/admin/orders/${encodeURIComponent(event.aggregate_id)}`}>{event.aggregate_id}</Link></Descriptions.Item><Descriptions.Item label="事件类型">{event.event_type}</Descriptions.Item><Descriptions.Item label="Topic">{event.topic}</Descriptions.Item><Descriptions.Item label="Tag">{event.tag}</Descriptions.Item><Descriptions.Item label="消息键">{event.message_key}</Descriptions.Item><Descriptions.Item label="去重键">{event.dedup_key}</Descriptions.Item><Descriptions.Item label="尝试次数">{event.attempts} / {event.max_attempts}</Descriptions.Item><Descriptions.Item label="下次重试">{formatDateTime(event.next_retry_at)}</Descriptions.Item><Descriptions.Item label="锁截止">{formatDateTime(event.locked_until)}</Descriptions.Item><Descriptions.Item label="发送时间">{formatMilliseconds(event.published_at)}</Descriptions.Item><Descriptions.Item label="创建时间">{formatDateTime(event.created_at)}</Descriptions.Item></Descriptions><h2>事件载荷</h2><pre className="admin-payload">{payloadText(event.payload || '')}</pre></Card>
  </>}</>
}
