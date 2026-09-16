import { useState, type ReactNode } from 'react'
import { Link, NavLink, useLocation, useNavigation } from 'react-router-dom'
import { Avatar, Button, Drawer, Dropdown, Spin } from 'antd'
import { HomeOutlined, ShoppingOutlined, ThunderboltOutlined, RobotOutlined, UserOutlined, LogoutOutlined, ProfileOutlined, MenuOutlined, DownOutlined } from '@ant-design/icons'
import { Brand } from './Brand'
import { useAuth } from '@/hooks/useAuth'
import { authPath } from '@/utils/authNavigation'

const navigation = [
  { to: '/', label: '首页', icon: <HomeOutlined /> },
  { to: '/recommend', label: '预算推荐', icon: <RobotOutlined /> },
  { to: '/products', label: '发现商品', icon: <ShoppingOutlined /> },
  { to: '/seckill', label: '限时秒杀', icon: <ThunderboltOutlined /> },
  { to: '/orders', label: '我的订单', icon: <ProfileOutlined /> },
]

export function AppLayout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const { state: navigationState } = useNavigation()
  const { userInfo, isAuthenticated, clearAuth } = useAuth()
  const [menuOpen, setMenuOpen] = useState(false)
  const returnTo = `${location.pathname}${location.search}${location.hash}`
  const links = navigation.map(({ to, label, icon }) => (
    <NavLink key={to} to={to} end={to === '/'} className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`} onClick={() => setMenuOpen(false)}>
      {icon}<span>{label}</span>
    </NavLink>
  ))

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到主要内容</a>
      <header className="site-header">
        <div className="header-inner">
          <Brand />
          <nav className="desktop-nav" aria-label="主导航">{links}</nav>
          <div className="header-actions">
            {isAuthenticated ? (
              <Dropdown trigger={['click']} placement="bottomRight" menu={{ items: [
                { key: 'profile', icon: <UserOutlined />, label: <Link to="/profile">个人中心</Link> },
                { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', onClick: clearAuth },
              ] }}>
                <button type="button" className="account-button" aria-label="账户菜单">
                  <Avatar size={30} src={userInfo?.avatar || undefined} icon={<UserOutlined />} />
                  <span className="account-name">{userInfo?.username || '我的账户'}</span><DownOutlined />
                </button>
              </Dropdown>
            ) : (
              <Link to={authPath('login', returnTo)}><Button type="primary">登录 / 注册</Button></Link>
            )}
            <Button className="mobile-menu-button" icon={<MenuOutlined />} aria-label="打开导航菜单" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)} />
          </div>
        </div>
      </header>
      <Drawer title="探索 BudgetMatch" open={menuOpen} onClose={() => setMenuOpen(false)} width="min(320px, 100vw)">
        <nav className="mobile-nav" aria-label="移动端导航">{links}</nav>
      </Drawer>
      <main id="main-content" tabIndex={-1} className="site-content" aria-busy={navigationState === 'loading'}>
        {navigationState === 'loading' && <div className="navigation-progress" role="status"><Spin size="small" /> 正在打开页面…</div>}
        {children}
      </main>
      <footer className="site-footer"><span>BudgetMatch © {new Date().getFullYear()}</span><span>让每一份预算，都有更好的选择。</span></footer>
    </div>
  )
}
