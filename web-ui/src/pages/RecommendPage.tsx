import { useState } from 'react'
import {
  Card,
  Form,
  Input,
  InputNumber,
  Button,
  Timeline,
  Spin,
  message,
  Tag,
  Row,
  Col,
  Typography,
  Space,
} from 'antd'
import { RobotOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { recommendStream } from '@/api/agent'
import { PriceDisplay } from '@/components/PriceDisplay'
import type { AgentRecommendResp, AgentBundleItem } from '@/types/api'

const { Title, Paragraph } = Typography

interface StreamEvent {
  event: string
  data: unknown
  time: string
}

export default function RecommendPage() {
  const [form] = Form.useForm()
  const [events, setEvents] = useState<StreamEvent[]>([])
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<AgentRecommendResp | null>(null)
  const [abortController, setAbortController] = useState<AbortController | null>(null)

  const handleRecommend = async (values: {
    query: string
    budget?: number
    max_items?: number
  }) => {
    if (abortController) {
      abortController.abort()
    }
    const controller = new AbortController()
    setAbortController(controller)

    setEvents([])
    setResult(null)
    setLoading(true)

    try {
      const budgetCents = values.budget ? Math.round(values.budget * 100) : undefined
      const stream = recommendStream(
        {
          query: values.query,
          budget_cents: budgetCents,
          max_items: values.max_items,
        },
        controller.signal
      )

      let finalResult: AgentRecommendResp | null = null

      for await (const evt of stream) {
        setEvents((prev) => [...prev, { ...evt, time: new Date().toLocaleTimeString() }])
        if (evt.event === 'recommendation.final' && typeof evt.data === 'object') {
          finalResult = evt.data as AgentRecommendResp
        }
      }

      setResult(finalResult)
    } catch (err) {
      if ((err as Error).name !== 'AbortError') {
        message.error((err as Error).message || '推荐失败')
      }
    } finally {
      setLoading(false)
      setAbortController(null)
    }
  }

  const getEventColor = (eventName: string) => {
    if (eventName === 'error') return 'red'
    if (eventName === 'done') return 'green'
    return 'blue'
  }

  const renderResultItem = (item: AgentBundleItem) => (
    <Card key={item.id} className="h-full">
      <div className="space-y-2">
        <div className="font-bold text-lg">{item.name}</div>
        <div className="flex justify-between">
          <Tag color="blue">{item.category}</Tag>
          <PriceDisplay cents={item.price_cents} />
        </div>
        <div className="text-gray-500 text-sm">库存: {item.stock}</div>
        <div className="text-gray-500 text-sm">评分: {item.score.toFixed(2)}</div>
        <Paragraph ellipsis={{ rows: 3 }} className="text-gray-600">{item.reason}</Paragraph>
      </div>
    </Card>
  )

  return (
    <div className="space-y-6">
      <div className="text-center py-8 bg-gradient-to-r from-blue-500 to-indigo-600 rounded-xl text-white">
        <Title level={2} className="!text-white">
          <RobotOutlined /> AI 预算推荐
        </Title>
        <Paragraph className="text-lg text-blue-100">
          告诉我你的预算和需求，AI 在严格预算约束下为你挑选最优购物方案
        </Paragraph>
      </div>

      <Card title="输入需求">
        <Form
          form={form}
          layout="vertical"
          onFinish={handleRecommend}
          initialValues={{ query: '', budget: 5000, max_items: 3 }}
        >
          <Form.Item
            label="需求描述"
            name="query"
            rules={[{ required: true, message: '请输入你的购物需求' }]}
          >
            <Input.TextArea
              rows={3}
              placeholder="例如：预算5000买手机，要求性价比高的"
              size="large"
            />
          </Form.Item>

          <Row gutter={24}>
            <Col span={12}>
              <Form.Item label="预算（元）" name="budget">
                <InputNumber
                  min={1}
                  prefix="¥"
                  className="w-full"
                  size="large"
                  placeholder="留空则不限制"
                />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="最多推荐件数" name="max_items">
                <InputNumber min={1} max={10} className="w-full" size="large" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              icon={loading ? <Spin size="small" /> : <PlayCircleOutlined />}
              loading={loading}
            >
              {loading ? '推荐中...' : '开始推荐'}
            </Button>
          </Form.Item>
        </Form>
      </Card>

      {events.length > 0 && (
        <Card title="推荐过程（SSE 实时流）">
          <Timeline
            items={events.map((evt) => ({
              color: getEventColor(evt.event),
              children: (
                <div className="text-sm">
                  <span className="text-gray-400 mr-2">[{evt.time}]</span>
                  <Tag color={getEventColor(evt.event)}>{evt.event}</Tag>
                  <span className="ml-2 text-gray-600">
                    {typeof evt.data === 'string'
                      ? evt.data
                      : JSON.stringify(evt.data).slice(0, 200)}
                  </span>
                </div>
              ),
            }))}
          />
        </Card>
      )}

      {result && (
        <Card title="推荐结果">
          <div className="mb-4 p-4 bg-blue-50 rounded">
            <div className="font-medium">
              意图解析：预算{' '}
              <PriceDisplay cents={result.intent?.budget_cents || 0} />，最多{' '}
              {result.intent?.max_items || '-'} 件
            </div>
            <Space className="mt-2">
              {result.intent?.keywords?.map((k) => (
                <Tag key={k}>{k}</Tag>
              ))}
            </Space>
            <Paragraph className="mt-2">{result.summary}</Paragraph>
          </div>

          <Row gutter={[24, 24]}>
            {result.items?.map(renderResultItem)}
          </Row>

          <div className="mt-6 pt-4 border-t flex justify-between items-center">
            <div className="text-gray-500">
              共 {result.items?.length || 0} 件商品
            </div>
            <div className="text-xl">
              总价: <PriceDisplay cents={result.total_price_cents} />
            </div>
          </div>
        </Card>
      )}
    </div>
  )
}
