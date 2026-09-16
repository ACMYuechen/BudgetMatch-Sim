import { useState } from 'react'
import { useLocation, Link } from 'react-router-dom'
import { Alert, Form, Input, Button, Tabs } from 'antd'
import { UserOutlined, LockOutlined, MailOutlined, ArrowRightOutlined } from '@ant-design/icons'
import { loginByUsername, loginByEmail } from '@/api/auth'
import { ApiError } from '@/api/request'
import { getUserInfo } from '@/api/user'
import { useAuthStore } from '@/stores/authStore'
import { AuthLayout } from '@/components/AuthLayout'
import { authPath, getReturnTo } from '@/utils/authNavigation'
import type { UserInfo } from '@/types/api'

interface LoginForm { account: string; password: string }

export default function LoginPage() {
  const { search } = useLocation()
  const returnTo = getReturnTo(search)
  const setAuth = useAuthStore((state) => state.setAuth)
  const [form] = Form.useForm<LoginForm>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [loginType, setLoginType] = useState<'username' | 'email'>('username')

  const handleLogin = async (values: LoginForm) => {
    setLoading(true)
    setError('')
    try {
      const result = loginType === 'username'
        ? await loginByUsername(values.account.trim(), values.password)
        : await loginByEmail(values.account.trim(), values.password)
      if (!result?.token) throw new Error('暂时无法完成登录，请稍后重试')

      // 资料请求使用新凭证，完成后再切换登录状态，避免页面提前卸载。
      let user: UserInfo | null = null
      try {
        user = await getUserInfo(undefined, result.token)
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) throw err
      }
      setAuth(result.token, user)
      // GuestOnly 使用相同 redirect 完成跳转。
    } catch (err) {
      setError((err as Error).message || '登录失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout title="欢迎回来" description="登录后，继续你的购物计划。">
      {returnTo !== '/' && <p className="auth-return-hint">登录后将返回刚才访问的页面，继续你的操作。</p>}
      <Tabs activeKey={loginType} onChange={(key) => {
        setLoginType(key as 'username' | 'email')
        setError('')
        form.resetFields(['account'])
      }} items={[
        { key: 'username', label: '用户名登录', disabled: loading },
        { key: 'email', label: '邮箱登录', disabled: loading },
      ]} />
      {error && <Alert type="error" showIcon message={error} className="form-alert" />}
      <Form form={form} name="login" layout="vertical" onFinish={handleLogin} disabled={loading}>
        <Form.Item name="account" label={loginType === 'username' ? '用户名' : '邮箱'} rules={[
          { required: true, whitespace: true, message: loginType === 'username' ? '请输入用户名' : '请输入邮箱' },
          ...(loginType === 'email' ? [{ type: 'email' as const, message: '请输入有效的邮箱地址' }] : []),
        ]} normalize={(value: string) => value.trim()}>
          <Input prefix={loginType === 'username' ? <UserOutlined /> : <MailOutlined />} placeholder={loginType === 'username' ? '你的用户名' : 'name@example.com'} autoComplete="username" autoCapitalize="none" size="large" />
        </Form.Item>
        <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
          <Input.Password prefix={<LockOutlined />} placeholder="输入密码" autoComplete="current-password" size="large" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={loading} block size="large" icon={<ArrowRightOutlined aria-hidden />}>登录</Button>
      </Form>
      <p className="auth-switch">还没有账户？<Link to={authPath('register', returnTo)}>创建账户</Link></p>
    </AuthLayout>
  )
}
