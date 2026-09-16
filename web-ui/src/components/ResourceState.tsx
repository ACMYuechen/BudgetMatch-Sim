import { Alert, Button, Spin } from 'antd'

export function ResourceState({ loading, error, retry }: { loading: boolean; error?: Error; retry: () => void }) {
  if (loading) return <div className="resource-loading" role="status" aria-label="正在加载"><Spin size="large" /><span>正在加载，请稍候…</span></div>
  if (error) return <Alert type="error" showIcon message="加载失败" description={error.message} action={<Button onClick={retry}>重试</Button>} />
  return null
}
