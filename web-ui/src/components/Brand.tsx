import { Link } from 'react-router-dom'
import { ShoppingOutlined } from '@ant-design/icons'

export function Brand() {
  return (
    <Link className="brand" to="/" aria-label="BudgetMatch 首页">
      <span className="brand-mark"><ShoppingOutlined /></span>
      <span>Budget<span className="brand-accent">Match</span><small>让预算，刚好合适</small></span>
    </Link>
  )
}
