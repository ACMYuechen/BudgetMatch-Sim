import { Link, useLocation, useNavigate } from 'react-router-dom'
import { Layout, Menu, Button, Space, Avatar, Dropdown } from 'antd'
import {
  HomeOutlined,
  ShoppingOutlined,
  ThunderboltOutlined,
  RobotOutlined,
  UserOutlined,
  LogoutOutlined,
  ShoppingCartOutlined,
} from '@ant-design/icons'
import { useAuthStore } from '@/stores/authStore'
import type { ReactNode } from 'react'

const { Header, Content, Footer } = Layout

interface AppLayoutProps {
  children: ReactNode
}

export function AppLayout({ children }: AppLayoutProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { userInfo, isAuthenticated, clearAuth } = useAuthStore()

  const menuItems = [
    { key: '/', icon: <HomeOutlined />, label: <Link to="/">首页</Link> },
    { key: '/products', icon: <ShoppingOutlined />, label: <Link to="/products">商城</Link> },
    { key: '/orders', icon: <ShoppingCartOutlined />, label: <Link to="/orders">我的订单</Link> },
    { key: '/seckill', icon: <ThunderboltOutlined />, label: <Link to="/seckill">秒杀</Link> },
    { key: '/recommend', icon: <RobotOutlined />, label: <Link to="/recommend">AI推荐</Link> },
  ]

  const handleLogout = () => {
    clearAuth()
    navigate('/login')
  }

  const userMenuItems = [
    { key: 'profile', icon: <UserOutlined />, label: <Link to="/profile">个人中心</Link> },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', onClick: handleLogout },
  ]

  return (
    <Layout className="min-h-screen">
      <Header className="flex items-center justify-between bg-white shadow-sm px-6">
        <div className="flex items-center gap-8">
          <Link to="/" className="text-xl font-bold text-blue-600">
            BudgetMatch Sim
          </Link>
          <Menu
            mode="horizontal"
            selectedKeys={[location.pathname]}
            items={menuItems}
            className="border-b-0"
          />
        </div>
        <div>
          {isAuthenticated ? (
            <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
              <Space className="cursor-pointer">
                <Avatar icon={<UserOutlined />} />
                <span>{userInfo?.username || '用户'}</span>
              </Space>
            </Dropdown>
          ) : (
            <Space>
              <Button type="primary" onClick={() => navigate('/login')}>登录</Button>
              <Button onClick={() => navigate('/register')}>注册</Button>
            </Space>
          )}
        </div>
      </Header>
      <Content className="p-6 max-w-7xl mx-auto w-full">{children}</Content>
      <Footer className="text-center text-gray-500">
        BudgetMatch-Sim ©{new Date().getFullYear()} - 智能预算推荐实验平台
      </Footer>
    </Layout>
  )
}
