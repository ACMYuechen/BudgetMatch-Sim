import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeftOutlined, CheckOutlined } from '@ant-design/icons'
import { Brand } from './Brand'

export function AuthLayout({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <main className="auth-page">
      <header className="auth-header"><Brand /><Link className="quiet-link" to="/"><ArrowLeftOutlined /> 返回首页</Link></header>
      <div className="auth-grid">
        <section className="auth-story">
          <span className="eyebrow">给每一份预算，一个好方案</span>
          <h1>买得合适，<br />比买得更多重要。</h1>
          <p>从真实需要出发，把预算、偏好和选择放在一起考虑。</p>
          <ul className="auth-benefits">
            <li><CheckOutlined /> 按你的预算探索商品组合</li>
            <li><CheckOutlined /> 随时继续上次的推荐对话</li>
            <li><CheckOutlined /> 在一个地方查看购物订单</li>
          </ul>
          <div className="auth-note"><span>一份清晰的购物计划</span><strong>从「我需要什么」开始。</strong></div>
        </section>
        <section className="auth-panel" aria-labelledby="auth-title">
          <div className="auth-panel-heading"><h2 id="auth-title">{title}</h2><p>{description}</p></div>
          {children}
        </section>
      </div>
      <footer className="auth-footer">BudgetMatch · 把每一分预算花在心意上</footer>
    </main>
  )
}
