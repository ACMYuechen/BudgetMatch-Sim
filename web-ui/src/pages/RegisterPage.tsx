import { useEffect, useState } from 'react'
import { useNavigate, useLocation, Link } from 'react-router-dom'
import { Alert, Form, Input, Button, message, Space } from 'antd'
import { UserOutlined, LockOutlined, MailOutlined, SafetyOutlined } from '@ant-design/icons'
import { register, sendCode } from '@/api/auth'
import { AuthLayout } from '@/components/AuthLayout'
import { authPath, getReturnTo } from '@/utils/authNavigation'

interface RegisterForm {
  username: string
  email: string
  password: string
  confirmPassword: string
  code: string
}

export default function RegisterPage() {
  const navigate = useNavigate()
  const { search } = useLocation()
  const returnTo = getReturnTo(search)
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const [error, setError] = useState('')
  const [form] = Form.useForm<RegisterForm>()

  useEffect(() => {
    if (countdown <= 0) return
    const timer = window.setTimeout(() => setCountdown((value) => value - 1), 1000)
    return () => window.clearTimeout(timer)
  }, [countdown])

  const handleSendCode = async () => {
    try {
      await form.validateFields(['email'])
    } catch {
      return
    }
    setSending(true)
    setError('')
    try {
      const result = await sendCode(form.getFieldValue('email'))
      if (!result.success) throw new Error('验证码发送失败，请稍后重试')
      message.success('验证码已发送，请查看邮箱')
      setCountdown(60)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSending(false)
    }
  }

  const handleRegister = async (values: RegisterForm) => {
    setLoading(true)
    setError('')
    try {
      const result = await register({
        username: values.username, email: values.email,
        password: values.password, code: values.code,
      })
      if (!result.success) throw new Error('注册未完成，请稍后重试')
      message.success('注册成功，请登录')
      navigate(authPath('login', returnTo), { replace: true })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthLayout title="创建你的账户" description="把喜欢的选择，放进合适的预算里。">
      {error && <Alert type="error" showIcon message={error} className="form-alert" />}
      <Form form={form} name="register" onFinish={handleRegister} layout="vertical" disabled={loading}>
        <Form.Item label="用户名" name="username" normalize={(value: string) => value.trim()} rules={[
          { required: true, whitespace: true, message: '请输入用户名' },
          { min: 2, message: '用户名至少 2 个字符' },
        ]}>
          <Input prefix={<UserOutlined />} placeholder="至少 2 个字符" autoComplete="username" size="large" />
        </Form.Item>
        <Form.Item label="邮箱" name="email" normalize={(value: string) => value.trim()} rules={[
          { required: true, message: '请输入邮箱' }, { type: 'email', message: '请输入有效的邮箱地址' },
        ]}>
          <Input prefix={<MailOutlined />} placeholder="用于接收注册验证码" autoComplete="email" size="large" />
        </Form.Item>
        <Form.Item label="邮箱验证码" required>
          <Space.Compact className="w-full">
            <Form.Item name="code" noStyle rules={[
              { required: true, message: '请输入验证码' }, { len: 6, message: '验证码为 6 位字符' },
            ]}>
              <Input aria-label="邮箱验证码" prefix={<SafetyOutlined />} placeholder="6 位验证码" maxLength={6} autoComplete="one-time-code" size="large" />
            </Form.Item>
            <Button size="large" loading={sending} disabled={countdown > 0 || loading} onClick={handleSendCode}>
              {countdown > 0 ? `${countdown}s 后重试` : '获取验证码'}
            </Button>
          </Space.Compact>
        </Form.Item>
        <div className="password-fields">
          <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }, { min: 6, message: '密码至少 6 个字符' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="至少 6 个字符" autoComplete="new-password" size="large" />
          </Form.Item>
          <Form.Item label="确认密码" name="confirmPassword" dependencies={['password']} rules={[
            { required: true, message: '请再次输入密码' },
            ({ getFieldValue }) => ({ validator(_, value) {
              return !value || getFieldValue('password') === value ? Promise.resolve() : Promise.reject(new Error('两次输入的密码不一致'))
            } }),
          ]}>
            <Input.Password placeholder="再次输入密码" autoComplete="new-password" size="large" />
          </Form.Item>
        </div>
        <Button type="primary" htmlType="submit" loading={loading} block size="large">创建账户</Button>
      </Form>
      <p className="auth-switch">已经有账户？<Link to={authPath('login', returnTo)}>去登录</Link></p>
    </AuthLayout>
  )
}
