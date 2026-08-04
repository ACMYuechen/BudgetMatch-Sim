import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { Card, Form, Input, Button, Tabs, message } from 'antd'
import { UserOutlined, LockOutlined, MailOutlined } from '@ant-design/icons'
import { loginByUsername, loginByEmail } from '@/api/auth'
import { getUserInfo } from '@/api/user'
import { useAuthStore } from '@/stores/authStore'

interface LoginForm {
  account: string
  password: string
}

export default function LoginPage() {
  const navigate = useNavigate()
  const { setAuth } = useAuthStore()
  const [loading, setLoading] = useState(false)
  const [loginType, setLoginType] = useState<'username' | 'email'>('username')

  const handleLogin = async (values: LoginForm) => {
    setLoading(true)
    try {
      const loginResp =
        loginType === 'username'
          ? await loginByUsername(values.account, values.password)
          : await loginByEmail(values.account, values.password)

      console.log('[Login] success:', loginResp)

      if (!loginResp?.token) {
        throw new Error('登录响应缺少 token，请检查后端接口')
      }

      // 关键：先存 token，后续请求才能带上 Authorization
      setAuth(loginResp.token, {
        user_id: loginResp.user_id,
        username: values.account,
        email: loginType === 'email' ? values.account : '',
        avatar: '',
        phone: '',
        role: loginResp.role,
      })

      // 获取完整用户信息，失败不影响登录流程
      try {
        const userInfo = await getUserInfo()
        setAuth(loginResp.token, { ...userInfo, role: loginResp.role })
      } catch (err) {
        console.error('[Login] getUserInfo failed:', err)
      }

      message.success('登录成功')
      navigate('/')
    } catch (err) {
      console.error('[Login] failed:', err)
      const errorMsg = (err as Error).message
      if (/不存在|not_found/i.test(errorMsg)) {
        message.error('账号不存在')
      } else if (/密码错误|账号或密码错误|invalid_password/i.test(errorMsg)) {
        message.error('账号或密码错误')
      } else if (/status \d{3}/.test(errorMsg) || /Request failed/.test(errorMsg) || /network/i.test(errorMsg)) {
        message.error('认证失败，请检查账号或密码')
      } else {
        message.error(errorMsg)
      }
     
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100">
      <Card title="用户登录" className="w-[420px]">
        <Tabs
          activeKey={loginType}
          onChange={(key) => setLoginType(key as 'username' | 'email')}
          items={[
            { key: 'username', label: '用户名登录' },
            { key: 'email', label: '邮箱登录' },
          ]}
          className="mb-4"
        />

        <Form name="login" onFinish={handleLogin} autoComplete="off">
          <Form.Item
            name="account"
            rules={[{ required: true, message: '请输入账号' }]}
          >
            <Input
              prefix={loginType === 'username' ? <UserOutlined /> : <MailOutlined />}
              placeholder={loginType === 'username' ? '用户名' : '邮箱'}
              size="large"
            />
          </Form.Item>

          <Form.Item
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="密码"
              size="large"
            />
          </Form.Item>

          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block size="large">
              登录
            </Button>
          </Form.Item>
        </Form>

        <div className="text-center">
          还没有账号？<Link to="/register" className="text-blue-600">立即注册</Link>
        </div>
      </Card>
    </div>
  )
}
