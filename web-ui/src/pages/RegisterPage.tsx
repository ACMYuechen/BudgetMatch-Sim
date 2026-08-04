import { useEffect, useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { Card, Form, Input, Button, message, Space } from 'antd'
import { UserOutlined, LockOutlined, MailOutlined, SafetyOutlined } from '@ant-design/icons'
import { register, sendCode, checkUsername } from '@/api/auth'

interface RegisterForm {
  username: string
  email: string
  password: string
  confirmPassword: string
  code: string
}

export default function RegisterPage() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const [form] = Form.useForm()

  useEffect(() => {
    if (countdown <= 0) return

    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          clearInterval(timer)
          return 0
        }
        return prev - 1
      })
    }, 1000)

    return () => clearInterval(timer)
  }, [countdown])

  const handleSendCode = async () => {
    const email = form.getFieldValue('email')
    const username = form.getFieldValue('username')
    if (!email) {
      message.error('请先输入邮箱')
      return
    }
    if (!username) {
      message.error('请先输入用户名')
      return
    }
    setSending(true)
    try {
      // 先检查用户名是否已被使用（必须检查成功后才能继续）
      const checkResp = await checkUsername(username)
      if (checkResp.exists) {
        message.error('该用户名已被使用')
        return
      }
      // 用户名可用，发送验证码
      await sendCode(email)
      message.success('验证码已发送')
      setCountdown(60)
    } catch (err) {
      const errorMsg = (err as Error).message
      if (/已存在|已被使用|already_exists/i.test(errorMsg)) {
        message.error('该用户名已被使用')
      } else if (/status \d{3}/.test(errorMsg) || /Request failed/.test(errorMsg) || /network/i.test(errorMsg)) {
        message.error('验证码发送失败，请稍后重试')
      } else {
        message.error(errorMsg)
      }
    } finally {
      setSending(false)
    }
  }

  const handleRegister = async (values: RegisterForm) => {
    setLoading(true)
    try {
      await register({
        username: values.username,
        email: values.email,
        password: values.password,
        code: values.code,
      })
      message.success('注册成功，请登录')
      navigate('/login')
    } catch (err) {
      const errorMsg = (err as Error).message
      if (/已存在|已被使用|already_exists/i.test(errorMsg)) {
        message.error('该用户名已被使用')
      } else if (/status \d{3}/.test(errorMsg) || /Request failed/.test(errorMsg) || /network/i.test(errorMsg)) {
        message.error('注册失败，请检查验证码是否正确或稍后重试')
      } else {
        message.error(errorMsg)
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100 py-8">
      <Card title="用户注册" className="w-[480px]">
        <Form
          form={form}
          name="register"
          onFinish={handleRegister}
          autoComplete="off"
          layout="vertical"
        >
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input prefix={<UserOutlined />} placeholder="用户名" size="large" />
          </Form.Item>

          <Form.Item
            label="邮箱"
            name="email"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '邮箱格式不正确' },
            ]}
          >
            <Input prefix={<MailOutlined />} placeholder="邮箱" size="large" />
          </Form.Item>

          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="密码" size="large" />
          </Form.Item>

          <Form.Item
            label="确认密码"
            name="confirmPassword"
            dependencies={['password']}
            rules={[
              { required: true, message: '请确认密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'))
                },
              }),
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="确认密码" size="large" />
          </Form.Item>

          <Form.Item
            label="验证码"
            name="code"
            rules={[{ required: true, message: '请输入验证码' }]}
          >
            <Space.Compact className="w-full">
              <Input
                prefix={<SafetyOutlined />}
                placeholder="验证码"
                size="large"
                className="flex-1"
              />
              <Button
                size="large"
                loading={sending}
                disabled={countdown > 0}
                onClick={handleSendCode}
              >
                {countdown > 0 ? `${countdown}s 后重试` : '获取验证码'}
              </Button>
            </Space.Compact>
          </Form.Item>

          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block size="large">
              注册
            </Button>
          </Form.Item>
        </Form>

        <div className="text-center">
          已有账号？<Link to="/login" className="text-blue-600">立即登录</Link>
        </div>
      </Card>
    </div>
  )
}
