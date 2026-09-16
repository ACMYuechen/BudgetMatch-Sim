import { useCallback } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { Button, Result } from 'antd'
import { Brand } from '@/components/Brand'
import { ResourceState } from '@/components/ResourceState'
import { useAuthStore } from '@/stores/authStore'
import { getUserInfo } from '@/api/user'
import { useResource } from '@/hooks/useResource'
import { isAdmin } from '@/utils/admin'

export default function AdminLayout() {
  const token = useAuthStore((state) => state.token)
  const clearAuth = useAuthStore((state) => state.clearAuth)
  const load = useCallback((signal: AbortSignal) => { void token; return getUserInfo(signal) }, [token])
  const { data, loading, error, reload } = useResource(load)
  return <div className="admin-shell"><a className="skip-link" href="#admin-main">跳到管理内容</a><header className="admin-header"><Brand /><span>管理工作台</span><div className="admin-actions"><Link to="/">返回商城</Link><Button onClick={clearAuth}>退出登录</Button></div></header><div className="admin-layout">
    <nav className="admin-nav" aria-label="管理导航">{[['products', '商品与规格'], ['activities', '秒杀活动'], ['orders', '商城订单'], ['outbox', '消息 Outbox']].map(([path, title]) => <NavLink key={path} to={`/admin/${path}`} className={({ isActive }) => isActive ? 'active' : ''}>{title}</NavLink>)}</nav>
    <main className="admin-main" id="admin-main" tabIndex={-1}><ResourceState loading={loading} error={error} retry={reload} />{data && (isAdmin(data.role) ? <Outlet /> : <Result status="403" title="没有管理权限" subTitle="仅管理员可以进入此工作台；账户角色以服务端查询为准。" extra={<Link to="/">返回商城</Link>} />)}</main>
  </div><footer className="admin-footer">所有操作以服务端权限与校验为准 · 请谨慎修改线上数据</footer></div>
}
