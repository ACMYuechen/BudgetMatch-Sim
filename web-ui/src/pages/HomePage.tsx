import { Link, useNavigate } from 'react-router-dom'
import { Button, Form, Input, InputNumber } from 'antd'
import { ArrowRightOutlined, BulbOutlined, CheckOutlined, ProfileOutlined, ShoppingOutlined, ThunderboltOutlined } from '@ant-design/icons'

interface ShoppingIdea { query: string; budget?: number }

const ideas = [
  { label: '宿舍学习', query: '想配一套宿舍学习设备，优先静音和性价比', budget: 3000 },
  { label: '轻装通勤', query: '想选一副通勤耳机，优先便携和续航', budget: 800 },
  { label: '桌面升级', query: '想升级办公桌面的键盘和鼠标，优先舒适和耐用', budget: 1500 },
]

const entries = [
  { to: '/products', icon: <ShoppingOutlined />, title: '先逛逛，再决定', text: '浏览在售商品，找到你感兴趣的选择。', label: '发现商品', style: 'mint' },
  { to: '/orders', icon: <ProfileOutlined />, title: '每一笔，都心中有数', text: '查看订单进度，继续未完成的购物。', label: '我的订单', style: 'sand' },
  { to: '/seckill', icon: <ThunderboltOutlined />, title: '好时机，好选择', text: '查看限时活动，发现预算内的惊喜。', label: '限时秒杀', style: 'lavender' },
]

export default function HomePage() {
  const navigate = useNavigate()
  const [form] = Form.useForm<ShoppingIdea>()

  const startPlan = ({ query, budget }: ShoppingIdea) => {
    const params = new URLSearchParams({ query: query.trim() })
    if (budget) params.set('budget_cents', String(Math.round(budget * 100)))
    navigate(`/recommend?${params}`)
  }

  return (
    <div className="home-page">
      <section className="home-hero">
        <div className="hero-copy">
          <span className="eyebrow"><span className="status-dot" /> 从需要出发的购物助手</span>
          <h1>你的预算，<br />值得<span>更好的选择。</span></h1>
          <p>告诉我们想买什么、愿意花多少。<br className="desktop-break" />一起把零散的想法，变成清晰的购物方案。</p>
          <div className="hero-values"><span><CheckOutlined /> 预算有数</span><span><CheckOutlined /> 推荐有据</span><span><CheckOutlined /> 对话可继续</span></div>
          <Link className="text-link" to="/products">也可以先逛逛商品 <ArrowRightOutlined /></Link>
        </div>
        <section className="plan-composer" aria-labelledby="plan-title">
          <div className="composer-heading"><span className="composer-icon"><BulbOutlined /></span><div><h2 id="plan-title">这次，想买点什么？</h2><p>从一个小想法开始。</p></div><span className="step-label">01 / 开始计划</span></div>
          <Form form={form} layout="vertical" onFinish={startPlan} initialValues={{ budget: 3000 }}>
            <Form.Item name="query" label="我的购物需求" rules={[{ required: true, whitespace: true, message: '先说说你想买什么吧' }]}>
              <Input.TextArea placeholder="例如：预算 3000 元，配一套安静、实用的宿舍学习设备…" autoSize={{ minRows: 3, maxRows: 6 }} maxLength={2000} showCount />
            </Form.Item>
            <div className="idea-presets" aria-label="试试这些购物场景"><span>试试看</span>{ideas.map((idea) => <button type="button" key={idea.label} onClick={() => form.setFieldsValue({ query: idea.query, budget: idea.budget })}>{idea.label} <ArrowRightOutlined /></button>)}</div>
            <Form.Item name="budget" label="这次的预算" extra="单位：元。也可以留空，在需求里告诉我。" className="budget-field">
              <InputNumber aria-label="这次的预算" min={1} max={1000000000} precision={2} prefix="¥" placeholder="输入预算" size="large" className="w-full" controls={false} />
            </Form.Item>
            <Button type="primary" htmlType="submit" block size="large" icon={<ArrowRightOutlined />}>开始我的预算计划</Button>
            <p className="composer-footnote">下一步可继续补充偏好，再生成推荐方案。</p>
          </Form>
        </section>
      </section>
      <section className="how-it-works" aria-label="如何开始">
        {[
          ['01', '说出你的需要', '预算、用途、偏好，都可以告诉我。'],
          ['02', '一起比较选择', '查看商品组合、价格和推荐理由。'],
          ['03', '慢慢选到合适', '继续追问，调整预算或换一种方案。'],
        ].map(([step, title, text]) => <div className="how-step" key={step}><span>{step}</span><div><h3>{title}</h3><p>{text}</p></div></div>)}
      </section>
      <section className="explore-section" aria-labelledby="explore-title">
        <div className="section-heading"><div><span className="eyebrow">不止一种开始方式</span><h2 id="explore-title">按照你的节奏，慢慢挑。</h2></div><span className="section-caption">从灵感，到你的下一份购物清单</span></div>
        <div className="explore-grid">{entries.map((entry) => <Link to={entry.to} className={`explore-card ${entry.style}`} key={entry.to}>
          <span className="explore-icon">{entry.icon}</span><h3>{entry.title}</h3><p>{entry.text}</p><span className="explore-action">{entry.label}<ArrowRightOutlined /></span>
        </Link>)}</div>
      </section>
    </div>
  )
}
