import { Button, Result, Space } from 'antd'

export default function RouteErrorPage() {
  return (
    <main className="route-error">
      <Result status="warning" title="页面暂时无法打开" subTitle="请重新加载试试，或返回首页继续浏览。" extra={
        <Space wrap>
          <Button type="primary" onClick={() => window.location.reload()}>重新加载</Button>
          <Button href="/">返回首页</Button>
        </Space>
      } />
    </main>
  )
}
