import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { Alert, Button, Card, Descriptions, Form, Input, Spin, Tag } from 'antd'
import { getSeckillOrder } from '@/api/seckill'
import { ApiError } from '@/api/request'
import { formatPrice } from '@/utils/format'
import { formatMilliseconds, seckillOrderStatus } from '@/utils/seckill'
import type { SeckillOrder } from '@/types/api'

function OrderResult({ id }: { id: string }) {
  const { state } = useLocation()
  const [result, setResult] = useState<SeckillOrder>()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(true)
  const [auto, setAuto] = useState(true)
  const [revision, setRevision] = useState(0)
  const stop = useRef<() => void>(() => {})
  const accepted = state?.accepted?.order_id === id ? state.accepted : undefined
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    let count = 0
    stop.current = () => { controller.abort(); clearTimeout(timer); setBusy(false); setAuto(false) }
    const query = async () => {
      if (controller.signal.aborted) return
      setBusy(true)
      setError('')
      count++
      let again = false
      try {
        const data = await getSeckillOrder(id, controller.signal)
        if (controller.signal.aborted) return
        if (data.order_id !== id) throw new Error('订单响应不匹配，请重新查询')
        setResult(data)
        again = data.status === 0
      } catch (err) {
        if (controller.signal.aborted) return
        again = err instanceof ApiError && err.status === 404
        setError(again ? '暂未查到订单，可能仍在排队或订单号不正确；这不代表抢购失败。' : err instanceof Error ? err.message : '查询失败，请重试')
      } finally {
        if (!controller.signal.aborted) {
          setBusy(false)
          // 网关按订单查询路径限制每分钟 10 次；保留余量且不重叠请求。
          if (again && count < 20) timer = setTimeout(query, 7000)
          else setAuto(false)
        }
      }
    }
    setAuto(true)
    void query()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [id, revision])
  const status = result?.status ?? accepted?.status
  return <Card title="秒杀订单结果" className="seckill-order-result">
    <p className="break-anywhere">订单号：{id}</p><Tag color={status === 1 || status === 3 ? 'green' : status === 2 ? 'red' : 'default'}>{status == null ? '结果待确认' : seckillOrderStatus[status] || `未知状态（${status}）`}</Tag>
    {busy && <span role="status"><Spin size="small" /> 正在查询结果</span>}
    {error && <Alert type="warning" showIcon message={error} />}
    {result && <Descriptions column={1}><Descriptions.Item label="活动"><Link to={`/seckill/${encodeURIComponent(result.activity_id)}`}>{result.activity_id}</Link></Descriptions.Item><Descriptions.Item label="商品规格">{result.sku_id}</Descriptions.Item><Descriptions.Item label="数量">{result.quantity}</Descriptions.Item><Descriptions.Item label="金额">{formatPrice(result.total_amount)}</Descriptions.Item><Descriptions.Item label="创建时间">{formatMilliseconds(result.created_at)}</Descriptions.Item></Descriptions>}
    <div className="admin-actions">{auto ? <Button onClick={() => stop.current()}>停止自动查询</Button> : <Button onClick={() => setRevision((value) => value + 1)} loading={busy}>重新查询结果</Button>}<Link to="/seckill">返回活动列表</Link></div>
    <p className="commerce-note">每轮查询结束后间隔 7 秒，最多自动查询 20 次；停止或达到上限不代表取消订单。请保留此页面地址以便再次查询。</p>
    <Alert type="info" showIcon message="秒杀订单独立于商城订单" description="当前秒杀接口没有提供付款入口，此处不会跳转商城付款，也不会把抢购成功显示成已支付。" />
  </Card>
}

export default function SeckillOrderPage() {
  const { orderId } = useParams()
  const navigate = useNavigate()
  return <div className="commerce-page"><div className="commerce-heading"><div><span className="eyebrow">结果清楚，安心等待</span><h1>秒杀结果查询</h1></div></div>{orderId ? <OrderResult key={orderId} id={orderId} /> : <Card><Form layout="vertical" onFinish={({ id }: { id: string }) => navigate(`/seckill/orders/${encodeURIComponent(id.trim())}`)}><Form.Item label="秒杀订单号" name="id" rules={[{ required: true, whitespace: true, message: '请输入秒杀订单号' }, { max: 128, message: '订单号过长' }]}><Input maxLength={128} placeholder="填写提交后返回的订单号" /></Form.Item><Button type="primary" htmlType="submit">查询结果</Button></Form><p className="commerce-note">只能查询本人订单。当前接口不提供秒杀订单列表。</p></Card>}</div>
}
