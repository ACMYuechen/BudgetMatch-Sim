import { useNavigate } from 'react-router-dom'
import { Card, Row, Col, Typography, Button, Space } from 'antd'
import {
  ShoppingOutlined,
  ThunderboltOutlined,
  RobotOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useAuthStore } from '@/stores/authStore'

const { Title, Paragraph } = Typography

export default function HomePage() {
  const navigate = useNavigate()
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)

  const features = [
    {
      icon: <RobotOutlined className="text-4xl text-blue-500" />,
      title: 'AI 预算推荐',
      desc: '基于 LLM + 规则引擎，在预算约束下给出最优购物方案',
      action: () => navigate('/recommend'),
      btnText: '立即体验',
    },
    {
      icon: <ShoppingOutlined className="text-4xl text-green-500" />,
      title: '商城购物',
      desc: '浏览商品、选择 SKU、创建订单',
      action: () => navigate('/products'),
      btnText: '去逛逛',
    },
    {
      icon: <ThunderboltOutlined className="text-4xl text-orange-500" />,
      title: '限时秒杀',
      desc: '参与秒杀活动，获取秒杀令牌后下单',
      action: () => navigate('/seckill'),
      btnText: '去秒杀',
    },
  ]

  return (
    <div className="space-y-8">
      <div className="text-center py-12 bg-gradient-to-r from-blue-500 to-indigo-600 rounded-xl text-white">
        <Title level={2} className="!text-white">
          BudgetMatch Sim 智能预算推荐
        </Title>
        <Paragraph className="text-lg text-blue-100">
          在严格预算约束下，通过 AI Agent 为你挑选最优个性化购物方案
        </Paragraph>
        <Space size="large" className="mt-6">
          <Button
            type="primary"
            size="large"
            icon={<RobotOutlined />}
            onClick={() => navigate('/recommend')}
          >
            AI 推荐
          </Button>
          <Button
            ghost
            size="large"
            icon={<SearchOutlined />}
            onClick={() => navigate('/products')}
          >
            浏览商品
          </Button>
          {!isAuthenticated && (
            <Button ghost size="large" onClick={() => navigate('/login')}>
              登录 / 注册
            </Button>
          )}
        </Space>
      </div>

      <Row gutter={[24, 24]}>
        {features.map((f) => (
          <Col xs={24} md={8} key={f.title}>
            <Card className="h-full text-center hover:shadow-lg transition-shadow">
              <div className="mb-4">{f.icon}</div>
              <Title level={4}>{f.title}</Title>
              <Paragraph className="text-gray-500">{f.desc}</Paragraph>
              <Button type="primary" onClick={f.action}>{f.btnText}</Button>
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  )
}
