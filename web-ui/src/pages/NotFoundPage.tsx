import { Button, Result } from 'antd'
import { Link } from 'react-router-dom'

export default function NotFoundPage() {
  return <Result status="404" title="这个页面不见了" subTitle="地址可能有误，也可能已经发生变化。回到首页，继续探索适合你的方案。" extra={<Link to="/"><Button type="primary">返回首页</Button></Link>} />
}
