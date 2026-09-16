import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { Alert, Button, Card, Empty, InputNumber, Modal, Radio, Tag } from 'antd'
import { acquireToken, getActivityDetail, getActivitySkus, submitSeckillOrder } from '@/api/seckill'
import { ProductImage } from '@/components/ProductImage'
import { ResourceState } from '@/components/ResourceState'
import { useResource } from '@/hooks/useResource'
import { useNow } from '@/hooks/useNow'
import { formatPrice } from '@/utils/format'
import { activityState, availableStock, countdown, formatMilliseconds } from '@/utils/seckill'
import type { Activity, SeckillSku } from '@/types/api'

function Participate({ activity, skus }: { activity: Activity; skus: SeckillSku[] }) {
  const navigate = useNavigate()
  const [selectedId, setSelectedId] = useState(() => skus.find((sku) => sku.status === 1 && availableStock(sku) > 0)?.id)
  const [quantity, setQuantity] = useState<number | null>(1)
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [uncertain, setUncertain] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const now = useNow()
  const state = activityState(activity, now)
  const sku = skus.find((item) => item.id === selectedId)
  const max = Math.min(99, sku ? availableStock(sku) : 0)
  const total = (sku?.seckill_price || 0) * (quantity || 0)
  const valid = state.open && sku?.status === 1 && quantity !== null && Number.isInteger(quantity) && quantity >= 1 && quantity <= max && Number.isSafeInteger(total) && total >= 0
  const submit = async () => {
    if (!valid || !sku || request.current || uncertain || !activityState(activity, Date.now()).open) return
    const controller = new AbortController()
    request.current = controller
    setBusy(true)
    setError('')
    let sent = false
    try {
      const { token } = await acquireToken({ activity_id: activity.id, sku_id: sku.id }, controller.signal)
      if (controller.signal.aborted) return
      if (!token) throw new Error('未取得抢购令牌，请稍后重试')
      if (!activityState(activity, Date.now()).open) throw new Error('活动已结束，未提交抢购')
      sent = true
      const result = await submitSeckillOrder({ activity_id: activity.id, sku_id: sku.id, quantity: quantity!, token }, controller.signal)
      if (controller.signal.aborted) return
      if (!result.order_id) throw new Error('没有收到订单号，提交结果待确认')
      navigate(`/seckill/orders/${encodeURIComponent(result.order_id)}`, { state: { accepted: result, activityId: activity.id } })
    } catch (err) {
      if (!controller.signal.aborted) { setError(err instanceof Error ? err.message : '请求失败，请稍后重试'); setUncertain(sent) }
    } finally {
      if (!controller.signal.aborted) { request.current = null; setBusy(false) }
    }
  }
  return <>
    <div className="seckill-countdown"><Tag color={state.open ? 'green' : 'default'}>{state.label}</Tag>{state.deadline > 0 && <strong>{state.open ? '距离结束' : '距离开始'} {countdown(state.deadline, now)}</strong>}</div>
    <Card title="选择秒杀商品" className="purchase-panel">
      {!skus.length ? <Empty description="暂无秒杀商品" /> : <><Radio.Group aria-label="秒杀商品" className="sku-options" value={selectedId} disabled={busy || uncertain} onChange={(event) => { setSelectedId(event.target.value); setQuantity(1) }}>{skus.map((item) => <Radio className="sku-option" key={item.id} value={item.id} disabled={item.status !== 1 || availableStock(item) <= 0}>
        <span className="sku-option-name"><strong>{item.title}</strong><span>{formatPrice(item.seckill_price)}</span></span>
        <span className="sku-specs">{item.subtitle}</span><span className="sku-specs">{item.status !== 1 ? '已下架' : `剩余参考库存 ${availableStock(item)}`}</span>
      </Radio>)}</Radio.Group>
      {sku && <><div className="seckill-selected"><ProductImage src={sku.pic} name={sku.title} /><div><h3>{sku.title}</h3><p>{sku.subtitle}</p><strong>{formatPrice(sku.seckill_price)}</strong>{sku.original_price > sku.seckill_price && <del>{formatPrice(sku.original_price)}</del>}</div></div><div className="purchase-quantity"><span>购买数量</span><InputNumber aria-label="秒杀数量" min={1} max={max} precision={0} value={quantity} onChange={setQuantity} disabled={busy || uncertain} /><span>单次最多 {max} 件</span></div><div className="purchase-total"><span>预计合计</span><strong>{formatPrice(total)}</strong></div></>}
      <Button type="primary" size="large" block disabled={!valid || uncertain} onClick={() => { setConfirm(true); setError('') }}>确认参与秒杀</Button></>}
      <p className="commerce-note">库存是查询时快照，单次最多 99 件是页面操作上限，并非每人限购规则。服务端将再次校验时间与库存。</p>
    </Card>
    <Modal title="确认秒杀请求" open={confirm} onCancel={() => setConfirm(false)} onOk={submit} okText="获取令牌并提交" confirmLoading={busy} okButtonProps={{ disabled: !valid || uncertain }} cancelButtonProps={{ disabled: busy }} closable={!busy} maskClosable={!busy} keyboard={!busy}>
      <p>{sku?.title} × {quantity}，预计 {formatPrice(total)}</p><p>提交后可能先进入排队，不代表秒杀成功。请等待结果，不要重复抢购。</p>
      {error && <Alert type="error" showIcon message={error} />}
      {uncertain && <Alert type="warning" showIcon message="提交结果不明确，已暂停再次提交" description={<><p>请求可能已经受理；没有订单号时当前接口无法按用户找回，请先联系管理员核对。刷新页面不会撤销请求。</p><Button onClick={() => { setUncertain(false); setConfirm(false) }}>我已核对，允许重新尝试</Button></>} />}
    </Modal>
    {uncertain && !confirm && <Alert type="warning" message="上次提交结果待确认" action={<Button onClick={() => setConfirm(true)}>查看处理说明</Button>} />}
  </>
}

export default function SeckillDetailPage() {
  const { id = '' } = useParams()
  const { state } = useLocation()
  const back = typeof state?.activityList === 'string' && /^\/seckill(?:\?|$)/.test(state.activityList) ? state.activityList : '/seckill'
  const load = useCallback(async (signal: AbortSignal) => {
    const [detail, skus] = await Promise.all([getActivityDetail(id, signal), getActivitySkus(id, signal)])
    return { activity: detail.activity, skus }
  }, [id])
  const { data, loading, error, reload } = useResource(load)
  return <div className="commerce-page"><Link className="commerce-text-link" to={back}>← 返回活动列表</Link><ResourceState loading={loading} error={error} retry={reload} />{data?.activity && <div className="seckill-detail"><section><span className="eyebrow">限时相遇，理性选择</span><h1>{data.activity.title}</h1><ProductImage src={data.activity.banner_url} name={data.activity.title} /><p className="seckill-description">{data.activity.description || '活动详情待补充'}</p><div className="seckill-times"><span>开始：{formatMilliseconds(data.activity.start_time)}</span><span>结束：{formatMilliseconds(data.activity.end_time)}</span></div></section><section><Participate key={id} activity={data.activity} skus={data.skus} /></section></div>}{data && !data.activity && <Empty description="活动不存在" />}</div>
}
