import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, Avatar, Button, Card, Descriptions, Skeleton, Space, Tag } from 'antd'
import { UserOutlined, ReloadOutlined, ProfileOutlined, RobotOutlined } from '@ant-design/icons'
import { getUserInfo } from '@/api/user'
import { useAuthStore } from '@/stores/authStore'

export default function ProfilePage() {
  const { token, userInfo, setAuth } = useAuthStore()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)

  useEffect(() => {
    if (!token) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    getUserInfo(controller.signal)
      .then((user) => {
        if (!controller.signal.aborted && useAuthStore.getState().token === token) setAuth(token, user)
      })
      .catch((err: Error) => {
        if (!controller.signal.aborted) setError(err.message)
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [token, setAuth, revision])

  return (
    <div className="profile-page">
      <div className="page-heading"><div><span className="eyebrow">你的 BudgetMatch</span><h1>个人中心</h1><p>查看账户信息，继续你的购物计划。</p></div>
        <Button icon={<ReloadOutlined />} loading={loading} onClick={() => setRevision((value) => value + 1)}>刷新信息</Button>
      </div>
      {error && <Alert className="form-alert" type="error" showIcon message="账户信息加载失败" description={error} action={<Button onClick={() => setRevision((value) => value + 1)}>重试</Button>} />}
      <Card>
        {loading ? <Skeleton avatar active paragraph={{ rows: 4 }} /> : userInfo ? (
          <>
            <div className="profile-identity"><Avatar size={64} src={userInfo.avatar || undefined} icon={<UserOutlined />} /><div><h2>{userInfo.username}</h2><Tag color="green">{userInfo.role >= 1 && userInfo.role <= 99 ? '管理员' : '普通用户'}</Tag></div></div>
            <Descriptions column={{ xs: 1, sm: 2 }} items={[
              { key: 'email', label: '邮箱', children: userInfo.email || '未设置' },
              { key: 'phone', label: '手机号', children: userInfo.phone || '未设置' },
              { key: 'id', label: '账户 ID', children: <span className="account-id">{userInfo.id}</span>, span: 2 },
            ]} />
          </>
        ) : <p>暂时没有可显示的账户信息，请点击刷新重试。</p>}
      </Card>
      <Card title="继续你的计划">
        <Space wrap size="middle">
          <Link to="/recommend"><Button type="primary" icon={<RobotOutlined />}>打开预算推荐</Button></Link>
          <Link to="/orders"><Button icon={<ProfileOutlined />}>查看我的订单</Button></Link>
        </Space>
      </Card>
    </div>
  )
}
