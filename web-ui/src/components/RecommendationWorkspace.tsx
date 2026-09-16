import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Alert, Button, Form, Input, InputNumber, Skeleton, Tag } from 'antd'
import { RobotOutlined, SendOutlined } from '@ant-design/icons'
import { v4 as uuidv4 } from 'uuid'
import { getConversationHistory, recommendStream, type AgentRecommendReq } from '@/api/agent'
import { useResource } from '@/hooks/useResource'
import { RecommendationResult } from './RecommendationResult'
import { formatPrice } from '@/utils/format'
import { isRecommendation, MAX_BUDGET_YUAN, recommendationError } from '@/utils/recommendation'
import type { AgentConversationTurn, AgentRecommendResp } from '@/types/api'

interface FormValues { query: string; budget?: number | null; max_items?: number | null }
interface Attempt extends AgentRecommendReq { conversation_id: string; turn_id: string }
interface Props {
  conversationId?: string
  initialQuery: string
  initialBudget?: number
  seed?: AgentConversationTurn
  onChanged: () => void
  onBusy: (busy: boolean) => void
}

export function RecommendationWorkspace({ conversationId, initialQuery, initialBudget, seed, onChanged, onBusy }: Props) {
  const [form] = Form.useForm<FormValues>()
  const navigate = useNavigate()
  const [newId] = useState(uuidv4)
  const load = useCallback(async (signal: AbortSignal) => conversationId
    ? getConversationHistory(conversationId, signal) : { conversation: null, turns: [] as AgentConversationTurn[] }, [conversationId])
  const history = useResource(load)
  const [localTurns, setLocalTurns] = useState<AgentConversationTurn[]>(() => seed ? [seed] : [])
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [pending, setPending] = useState<Attempt | null>(null)
  const [notice, setNotice] = useState<{ kind: 'error' | 'warning'; text: string } | null>(null)
  const active = useRef<AbortController | null>(null)
  const attempt = useRef<Attempt | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout>>()
  const transcriptRef = useRef<HTMLDivElement>(null)
  const turns = useMemo(() => [...new Map([...(history.data?.turns || []), ...localTurns].map((turn) => [turn.turn_id, turn])).values()].sort((a, b) => a.sequence - b.sequence), [history.data, localTurns])
  const latest = turns[turns.length - 1]
  const intent = latest?.result?.intent || history.data?.conversation?.state
  const blocked = !!conversationId && (history.loading || !!history.error)

  useEffect(() => { onBusy(busy); return () => onBusy(false) }, [busy, onBusy])
  useEffect(() => () => { active.current?.abort(); clearTimeout(timerRef.current) }, [])
  useEffect(() => {
    const element = transcriptRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [turns.length, busy])
  useEffect(() => {
    if (!busy && pending && history.data?.turns.some((turn) => turn.turn_id === pending.turn_id)) {
      attempt.current = null
      setPending(null)
      setNotice(null)
      if (form.getFieldValue('query')?.trim() === pending.query) form.setFieldsValue({ query: '', budget: undefined, max_items: undefined })
    }
  }, [history.data, pending, busy, form])

  const run = async (request: Attempt) => {
    if (active.current || blocked) return
    const controller = new AbortController()
    active.current = controller
    attempt.current = request
    setPending(request)
    setBusy(true)
    setNotice(null)
    setProgress('正在理解你的需求…')
    const startedAt = Date.now()
    timerRef.current = setTimeout(() => {
      if (active.current !== controller) return
      active.current = null
      controller.abort()
      setBusy(false)
      setNotice({ kind: 'error', text: '等待推荐超时，可重试原请求或先刷新会话确认结果。' })
    }, 120000)
    try {
      let result: AgentRecommendResp | undefined
      for await (const event of recommendStream(request, controller.signal)) {
        if (controller.signal.aborted || active.current !== controller) return
        if (event.event === 'rpc.started') setProgress('正在检索商品、核对预算并组合方案…')
        if (event.event === 'error') {
          const payload = event.data as { message?: string }
          throw new Error(payload?.message || '推荐服务暂时不可用，请重试')
        }
        if (event.event === 'recommendation.final') {
          if (!isRecommendation(event.data) || event.data.conversation_id !== request.conversation_id || event.data.turn_id !== request.turn_id) {
            throw new Error('推荐结果不完整或与本次请求不匹配，请重试原请求')
          }
          result = event.data
          break
        }
      }
      if (controller.signal.aborted || active.current !== controller) return
      if (!result) throw new Error('连接已结束，但没有收到完整方案。请重试原请求。')
      const completed: AgentConversationTurn = {
        turn_id: result.turn_id, sequence: (latest?.sequence || 0) + 1, query: request.query,
        budget_cents: request.budget_cents || 0, max_items: request.max_items || 0,
        intent: result.intent, result, created_at_ms: startedAt, completed_at_ms: Date.now(),
      }
      attempt.current = null
      setPending(null)
      form.resetFields()
      form.setFieldsValue({ query: '', budget: undefined, max_items: undefined })
      // final 已包含完整方案；不因随后读取历史失败而丢掉已经收到的结果。
      if (!conversationId) navigate(`/recommend/${encodeURIComponent(result.conversation_id)}`, { replace: true, state: { completedTurn: completed } })
      else setLocalTurns((items) => [...items.filter((item) => item.turn_id !== completed.turn_id), completed])
      onChanged()
    } catch (err) {
      if (!controller.signal.aborted && active.current === controller) setNotice({ kind: 'error', text: recommendationError(err) })
    } finally {
      if (active.current === controller) {
        clearTimeout(timerRef.current)
        active.current = null
        setBusy(false)
      }
    }
  }

  const send = (values: FormValues) => {
    const payload = {
      query: values.query.trim(),
      budget_cents: values.budget == null ? undefined : Math.round(values.budget * 100),
      max_items: values.max_items == null ? undefined : values.max_items,
      conversation_id: conversationId || newId,
    }
    const prior = attempt.current
    const same = prior && prior.query === payload.query && prior.budget_cents === payload.budget_cents && prior.max_items === payload.max_items
    void run({ ...payload, turn_id: same ? prior.turn_id : uuidv4() })
  }

  const stop = () => {
    active.current?.abort()
    active.current = null
    clearTimeout(timerRef.current)
    setBusy(false)
    setNotice({ kind: 'warning', text: '已停止等待。服务端可能已经完成处理，可刷新会话确认，或重试原请求。' })
  }

  return <section className="recommend-workspace" aria-label="预算推荐对话">
    <header className="recommend-workspace-heading"><div className="recommend-assistant-icon"><RobotOutlined aria-hidden /></div><div><h2>{latest?.result?.conversation_title || history.data?.conversation?.conversation_title || (conversationId ? '预算推荐会话' : '从一个想法开始')}</h2><p>说说你的需要，我们一起把预算安排好。</p></div>{conversationId && <Button size="small" onClick={history.reload} disabled={busy} loading={history.loading}>刷新历史</Button>}</header>
    <div className="recommend-transcript" ref={transcriptRef}>
      {history.loading && !turns.length ? <Skeleton active paragraph={{ rows: 6 }} /> : null}
      {history.error && <Alert type="error" showIcon message="会话历史加载失败" description="已收到的方案会保留在下方。请重试读取历史后继续对话。" action={<Button onClick={history.reload}>重试历史</Button>} />}
      {!history.loading && !history.error && !turns.length && <div className="recommend-welcome"><span className="eyebrow">预算推荐助手</span><h3>想买什么？慢慢说。</h3><p>告诉我用途、预算和在意的细节。可以继续追问，逐步调整方案。</p><div className="recommend-presets">{[
        ['宿舍学习', '想配一套宿舍学习用品，优先安静和实用', 3000],
        ['轻装通勤', '选一副轻便的通勤耳机，重视续航', 800],
        ['桌面升级', '改善桌面办公体验，优先性价比', 1500],
      ].map(([label, query, budget]) => <Button key={label} onClick={() => form.setFieldsValue({ query: String(query), budget: Number(budget) })}>{label}</Button>)}</div></div>}
      {turns.map((turn) => <article className="recommend-turn" key={turn.turn_id}>
        <div className="recommend-user-message"><span>你的需求 · 第 {turn.sequence} 轮</span><p>{turn.query}</p></div>
        <div className="recommend-answer-label"><RobotOutlined aria-hidden /> 预算助手</div>
        <RecommendationResult result={turn.result} />
      </article>)}
      {pending && <div className="recommend-pending"><div className="recommend-user-message"><span>本次需求</span><p>{pending.query}</p></div>{busy && <div className="recommend-progress" role="status"><span className="status-dot" />{progress}</div>}</div>}
      {notice && <Alert type={notice.kind} showIcon message={notice.text} description={<div className="recommend-recovery"><span>重试原请求会复用会话与轮次标识；修改内容后发送则视为新的请求。</span><div><Button disabled={busy || blocked} onClick={() => { if (attempt.current) void run(attempt.current) }}>重试原请求</Button>{pending && !conversationId && <Link to={`/recommend/${encodeURIComponent(pending.conversation_id)}`}>查看本次会话</Link>}</div></div>} />}
    </div>
    <div className="recommend-composer">
      {intent && <div className="recommend-current-intent"><Tag color="green">当前预算 {intent.budget_cents > 0 ? formatPrice(intent.budget_cents) : '未设定'}</Tag><Tag>最多 {intent.max_items} 件</Tag><span>留空将继承已有约束，填写则覆盖本轮。</span></div>}
      <Form form={form} layout="vertical" initialValues={{ query: initialQuery, budget: initialBudget }} onFinish={send} disabled={busy || blocked}>
        <Form.Item name="query" rules={[{ required: true, whitespace: true, message: '请输入你的购物需求' }, { max: 2000, message: '需求最多 2000 个字符' }]}>
          <Input.TextArea aria-label="购物需求" autoSize={{ minRows: 2, maxRows: 5 }} maxLength={2000} showCount placeholder={conversationId ? '继续追问，例如：预算不变，换成更轻便的款式' : '例如：预算 3000 元，配一套安静、实用的宿舍学习设备'} />
        </Form.Item>
        <div className="recommend-composer-controls">
          <Form.Item label="本轮预算（元）" name="budget" rules={[{ type: 'number', min: 1, max: MAX_BUDGET_YUAN, message: '请输入 1 至 10 亿元以内的预算' }]}><InputNumber min={1} max={MAX_BUDGET_YUAN} precision={2} prefix="¥" placeholder={intent?.budget_cents ? '继承上轮预算' : '留空从需求中解析'} /></Form.Item>
          <Form.Item label="最多件数" name="max_items" rules={[{ validator: (_, value) => value == null || (Number.isInteger(value) && value >= 1 && value <= 10) ? Promise.resolve() : Promise.reject(new Error('请输入 1 至 10 的整数')) }]}><InputNumber min={1} max={10} precision={0} placeholder={intent ? '继承上轮' : '默认 3'} /></Form.Item>
          <div className="recommend-send-actions">{busy ? <Button key="stop" htmlType="button" size="large" disabled={false} onClick={(event) => { event.preventDefault(); stop() }}>停止生成</Button> : <Button key="send" type="primary" size="large" htmlType="submit" icon={<SendOutlined aria-hidden />}>发送需求</Button>}</div>
        </div>
      </Form>
      <p className="commerce-note">推荐不等于下单。查看商品、确认规格后，才会进入购买流程。</p>
    </div>
  </section>
}
