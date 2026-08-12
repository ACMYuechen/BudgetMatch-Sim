import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  InputNumber,
  List,
  Popconfirm,
  Row,
  Skeleton,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  DeleteOutlined,
  MessageOutlined,
  PlusOutlined,
  RobotOutlined,
  SendOutlined,
} from '@ant-design/icons'
import { v4 as uuidv4 } from 'uuid'
import {
  deleteConversation,
  listConversations,
  listConversationTurns,
  recommendStream,
} from '@/api/agent'
import { PriceDisplay } from '@/components/PriceDisplay'
import type {
  AgentBundleItem,
  AgentConversationSummary,
  AgentConversationTurn,
  AgentRecommendResp,
} from '@/types/api'

const { Title, Paragraph, Text } = Typography

// 与后端 budget_cents 上限（10 亿元）保持一致，前端金额单位为元。
const MAX_BUDGET_YUAN = 1_000_000_000

interface RecommendFormValues {
  query: string
  budget?: number
  max_items?: number
}

interface StreamStatus {
  event: string
  label: string
}

function formatUpdatedAt(timestamp: number) {
  const date = new Date(timestamp)
  const today = new Date()
  if (date.toDateString() === today.toDateString()) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return date.toLocaleDateString([], { month: '2-digit', day: '2-digit' })
}

/** 渲染一轮保存下来的完整推荐结果，供实时结果和历史恢复共同复用。 */
function ResultCard({ result }: { result: AgentRecommendResp }) {
  return (
    <Card size="small" className="border-blue-100 bg-blue-50/40">
      <Space direction="vertical" size={12} className="w-full">
        <Paragraph className="!mb-0 whitespace-pre-wrap">{result.summary}</Paragraph>
        <Space wrap>
          <Tag color="blue">预算 <PriceDisplay cents={result.intent?.budget_cents || 0} /></Tag>
          <Tag>最多 {result.intent?.max_items || '-'} 件</Tag>
          {result.intent?.keywords?.map((keyword) => <Tag key={keyword}>{keyword}</Tag>)}
          {result.intent?.preferences?.map((preference) => <Tag color="cyan" key={preference}>{preference}</Tag>)}
        </Space>
        {result.items?.length > 0 && (
          <Row gutter={[12, 12]}>
            {result.items.map((item: AgentBundleItem) => (
              <Col xs={24} lg={12} key={item.id}>
                <Card size="small" className="h-full bg-white">
                  <div className="flex items-start justify-between gap-3">
                    <Text strong>{item.name}</Text>
                    <PriceDisplay cents={item.price_cents} />
                  </div>
                  <Space wrap className="mt-2">
                    <Tag>{item.category}</Tag>
                    <Text type="secondary">库存 {item.stock}</Text>
                    <Text type="secondary">评分 {item.score.toFixed(2)}</Text>
                  </Space>
                  {item.reason && <Paragraph type="secondary" className="!mb-0 !mt-2">{item.reason}</Paragraph>}
                </Card>
              </Col>
            ))}
          </Row>
        )}
        <div className="flex justify-between border-t border-blue-100 pt-3">
          <Text type="secondary">共 {result.items?.length || 0} 件</Text>
          <Text strong>总价 <PriceDisplay cents={result.total_price_cents || 0} /></Text>
        </div>
      </Space>
    </Card>
  )
}

/** 按用户消息、助手结果的顺序渲染一个不可变历史轮次。 */
function TurnView({ turn }: { turn: AgentConversationTurn }) {
  return (
    <div className="space-y-3" id={`turn-${turn.turn_id}`}>
      <div className="ml-auto max-w-3xl rounded-2xl rounded-br-sm bg-blue-600 px-4 py-3 text-white shadow-sm">
        <div className="whitespace-pre-wrap">{turn.query}</div>
      </div>
      <div className="max-w-4xl">
        <div className="mb-2 flex items-center gap-2 text-gray-500">
          <RobotOutlined />
          <span>预算推荐助手 · 第 {turn.sequence} 轮</span>
        </div>
        <ResultCard result={turn.result} />
      </div>
    </div>
  )
}

export default function RecommendPage() {
  const [form] = Form.useForm<RecommendFormValues>()
  const navigate = useNavigate()
  const { conversationId } = useParams<{ conversationId: string }>()
  const [conversations, setConversations] = useState<AgentConversationSummary[]>([])
  const [conversation, setConversation] = useState<AgentConversationSummary | null>(null)
  const [turns, setTurns] = useState<AgentConversationTurn[]>([])
  const [listLoading, setListLoading] = useState(true)
  const [historyLoading, setHistoryLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [streamStatus, setStreamStatus] = useState<StreamStatus | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const historyAbortRef = useRef<AbortController | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)

  // 后端接口分页返回；页面首次进入时拉取全部摘要，保证侧栏不会静默漏掉旧会话。
  const loadConversationList = useCallback(async () => {
    setListLoading(true)
    try {
      const firstPage = await listConversations()
      const allConversations = [...(firstPage.list || [])]
      const totalPages = Math.ceil(firstPage.total / firstPage.page_size)
      for (let page = 2; page <= totalPages; page += 1) {
        const nextPage = await listConversations(page, firstPage.page_size)
        allConversations.push(...(nextPage.list || []))
      }
      setConversations(allConversations)
    } catch (error) {
      message.error((error as Error).message || '加载会话失败')
    } finally {
      setListLoading(false)
    }
  }, [])

  // 历史轮次同样逐页恢复，结果按后端 sequence 正序直接展示。
  const loadHistory = useCallback(async (id: string, signal: AbortSignal) => {
    setHistoryLoading(true)
    try {
      const firstPage = await listConversationTurns(id, 1, 100, signal)
      const allTurns = [...(firstPage.list || [])]
      const totalPages = Math.ceil(firstPage.total / firstPage.page_size)
      for (let page = 2; page <= totalPages; page += 1) {
        const nextPage = await listConversationTurns(id, page, firstPage.page_size, signal)
        allTurns.push(...(nextPage.list || []))
      }
      if (signal.aborted) return
      setConversation(firstPage.conversation)
      setTurns(allTurns)
    } catch (error) {
      if (signal.aborted) return
      setConversation(null)
      setTurns([])
      message.error((error as Error).message || '加载对话历史失败')
      navigate('/recommend', { replace: true })
    } finally {
      // 只有当前请求可以结束 loading，防止旧请求先返回时解除新请求的加载状态。
      if (!signal.aborted) setHistoryLoading(false)
    }
  }, [navigate])

  useEffect(() => {
    void loadConversationList()
  }, [loadConversationList])

  useEffect(() => {
    abortRef.current?.abort()
    historyAbortRef.current?.abort()
    setStreamStatus(null)
    form.resetFields()
    if (conversationId) {
      const controller = new AbortController()
      historyAbortRef.current = controller
      void loadHistory(conversationId, controller.signal)
    } else {
      setConversation(null)
      setTurns([])
      setHistoryLoading(false)
    }
    return () => {
      historyAbortRef.current?.abort()
    }
  }, [conversationId, form, loadHistory])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: turns.length > 0 ? 'smooth' : 'auto', block: 'end' })
  }, [turns, submitting])

  const handleNewConversation = () => {
    abortRef.current?.abort()
    navigate('/recommend')
  }

  const handleDelete = async (id: string) => {
    try {
      await deleteConversation(id)
      setConversations((items) => items.filter((item) => item.conversation_id !== id))
      if (conversationId === id) navigate('/recommend', { replace: true })
      message.success('会话已删除')
    } catch (error) {
      message.error((error as Error).message || '删除会话失败')
    }
  }

  const handleRecommend = async (values: RecommendFormValues) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setSubmitting(true)
    setStreamStatus({ event: 'request.accepted', label: '正在理解你的需求…' })

    try {
      // 每次点击生成新的 turn_id；网络层重试相同请求时后端只会保存一次。
      const stream = recommendStream({
        query: values.query,
        budget_cents: values.budget ? Math.round(values.budget * 100) : undefined,
        max_items: values.max_items,
        conversation_id: conversationId,
        turn_id: uuidv4(),
      }, controller.signal)
      let finalResult: AgentRecommendResp | null = null
      for await (const event of stream) {
        if (event.event === 'rpc.started') {
          setStreamStatus({ event: event.event, label: '正在检索商品并组合方案…' })
        } else if (event.event === 'recommendation.final') {
          finalResult = event.data as AgentRecommendResp
          setStreamStatus({ event: event.event, label: '方案已生成，正在保存会话…' })
        } else if (event.event === 'error') {
          const payload = event.data as { message?: string }
          throw new Error(payload.message || '推荐失败')
        }
      }
      if (!finalResult) throw new Error('未收到完整推荐结果')

      form.resetFields()
      const nextId = finalResult.conversation_id
      if (!conversationId || conversationId !== nextId) {
        navigate(`/recommend/${nextId}`, { replace: true })
      } else {
        // 当前路由不变时不会触发上方 effect，显式刷新并纳入同一套竞态取消机制。
        historyAbortRef.current?.abort()
        const historyController = new AbortController()
        historyAbortRef.current = historyController
        await loadHistory(nextId, historyController.signal)
        if (historyAbortRef.current === historyController) historyAbortRef.current = null
      }
      await loadConversationList()
    } catch (error) {
      if ((error as Error).name !== 'AbortError') {
        message.error((error as Error).message || '推荐失败')
      }
    } finally {
      if (abortRef.current === controller) abortRef.current = null
      setSubmitting(false)
      setStreamStatus(null)
    }
  }

  const state = conversation?.state

  return (
    <Row gutter={[20, 20]} className="min-h-[calc(100vh-160px)]">
      <Col xs={24} lg={7} xl={6}>
        <Card
          title={<Space><MessageOutlined />会话记录</Space>}
          extra={<Button type="primary" icon={<PlusOutlined />} onClick={handleNewConversation}>新对话</Button>}
          className="h-full"
          bodyStyle={{ padding: 8 }}
        >
          {listLoading ? <Skeleton active paragraph={{ rows: 6 }} /> : conversations.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有历史会话" />
          ) : (
            <List
              dataSource={conversations}
              renderItem={(item) => {
                const selected = item.conversation_id === conversationId
                return (
                  <List.Item
                    className={`cursor-pointer rounded-lg !px-3 transition-colors ${selected ? '!bg-blue-50' : 'hover:!bg-gray-50'}`}
                    onClick={() => navigate(`/recommend/${item.conversation_id}`)}
                    actions={[
                      <Popconfirm
                        key="delete"
                        title="删除这个会话？"
                        description="会话及全部轮次将一并删除，且无法恢复。"
                        okText="删除"
                        cancelText="取消"
                        okButtonProps={{ danger: true }}
                        onConfirm={(event) => { event?.stopPropagation(); void handleDelete(item.conversation_id) }}
                        onCancel={(event) => event?.stopPropagation()}
                      >
                        <Button type="text" danger size="small" aria-label={`删除会话 ${item.conversation_title}`} icon={<DeleteOutlined />} onClick={(event) => event.stopPropagation()} />
                      </Popconfirm>,
                    ]}
                  >
                    <List.Item.Meta
                      title={<Text strong={selected} ellipsis>{item.conversation_title || '未命名会话'}</Text>}
                      description={<Text type="secondary">{item.turn_count} 轮 · {formatUpdatedAt(item.updated_at_ms)}</Text>}
                    />
                  </List.Item>
                )
              }}
            />
          )}
        </Card>
      </Col>

      <Col xs={24} lg={17} xl={18}>
        <Card className="h-full" bodyStyle={{ padding: 0 }}>
          <div className="border-b px-5 py-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <Title level={4} className="!mb-1"><RobotOutlined className="mr-2 text-blue-600" />{conversation?.conversation_title || '新建预算推荐会话'}</Title>
                <Text type="secondary">结构化约束会跨轮继承；本轮填写的预算或件数会覆盖上轮。</Text>
              </div>
              {state && (
                <Space wrap>
                  <Tag color="blue">当前预算 <PriceDisplay cents={state.budget_cents} /></Tag>
                  <Tag>最多 {state.max_items} 件</Tag>
                </Space>
              )}
            </div>
          </div>

          <div className="max-h-[calc(100vh-430px)] min-h-[360px] space-y-7 overflow-y-auto bg-gray-50/40 p-5" aria-live="polite">
            {historyLoading ? <Skeleton active paragraph={{ rows: 8 }} /> : turns.length === 0 ? (
              <Empty description="描述你的购物目标，我会在预算内给出组合方案" />
            ) : turns.map((turn) => <TurnView key={turn.turn_id} turn={turn} />)}
            {submitting && (
              <Alert
                type="info"
                showIcon
                icon={<Spin size="small" />}
                message={streamStatus?.label || '正在生成推荐…'}
                description="请求已安全绑定本次轮次，网络重试不会重复生成记录。"
              />
            )}
            <div ref={bottomRef} />
          </div>

          <div className="border-t bg-white p-5">
            <Form form={form} layout="vertical" onFinish={handleRecommend} disabled={submitting}>
              <Form.Item name="query" rules={[{ required: true, whitespace: true, message: '请输入你的购物需求' }]} className="!mb-3">
                <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} maxLength={2000} showCount placeholder={conversation ? '继续追问，例如：预算不变，换成更轻便的款式' : '例如：预算 5000 元配一套宿舍学习设备，优先性价比和静音'} />
              </Form.Item>
              <div className="flex flex-wrap items-end justify-between gap-3">
                <Space wrap align="end">
                  <Form.Item label="本轮预算（元）" name="budget" className="!mb-0">
                    <InputNumber min={1} max={MAX_BUDGET_YUAN} precision={2} prefix="¥" placeholder={state?.budget_cents ? `继承 ¥${(state.budget_cents / 100).toFixed(2)}` : '留空自动解析'} />
                  </Form.Item>
                  <Form.Item label="最多件数" name="max_items" className="!mb-0">
                    <InputNumber min={1} max={10} placeholder={state?.max_items ? `继承 ${state.max_items}` : '默认 3'} />
                  </Form.Item>
                </Space>
                <Button type="primary" htmlType="submit" size="large" loading={submitting} icon={<SendOutlined />}>发送需求</Button>
              </div>
            </Form>
          </div>
        </Card>
      </Col>
    </Row>
  )
}
