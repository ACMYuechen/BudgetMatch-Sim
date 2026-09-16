import { useEffect } from 'react'
import { createBrowserRouter, Navigate, Outlet, useLocation } from 'react-router-dom'
import { AppLayout } from '@/components/AppLayout'
import { useAuthStore } from '@/stores/authStore'
import { authPath, getReturnTo } from '@/utils/authNavigation'
import RouteErrorPage from '@/pages/RouteErrorPage'

const pageTitles: Record<string, string> = {
  products: '发现商品', orders: '我的订单', seckill: '限时秒杀',
  recommend: '预算推荐', profile: '个人中心', login: '登录', register: '注册',
  admin: '管理工作台',
}

// eslint-disable-next-line react-refresh/only-export-components
function RouteFrame() {
  const { pathname } = useLocation()
  useEffect(() => {
    document.title = `${pageTitles[pathname.split('/')[1]] || '让预算，刚好合适'} · BudgetMatch`
    window.scrollTo(0, 0)
  }, [pathname])
  return <Outlet />
}

// eslint-disable-next-line react-refresh/only-export-components
function RequireAuth() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)
  const { pathname, search, hash } = useLocation()
  return isAuthenticated ? <Outlet /> : <Navigate to={authPath('login', `${pathname}${search}${hash}`)} replace />
}

// eslint-disable-next-line react-refresh/only-export-components
function GuestOnly() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)
  const { search } = useLocation()
  return isAuthenticated ? <Navigate to={getReturnTo(search)} replace /> : <Outlet />
}

const router = createBrowserRouter([{
  element: <RouteFrame />,
  errorElement: <RouteErrorPage />,
  children: [
    {
      element: <RequireAuth />,
      children: [{
        path: '/admin',
        lazy: async () => ({ Component: (await import('@/components/admin/AdminLayout')).default }),
        children: [
          { index: true, element: <Navigate to="products" replace /> },
          { path: 'products', lazy: async () => ({ Component: (await import('@/pages/admin/ProductsPage')).default }) },
          { path: 'products/:id', lazy: async () => ({ Component: (await import('@/pages/admin/ProductPage')).default }) },
          { path: 'activities', lazy: async () => ({ Component: (await import('@/pages/admin/ActivitiesPage')).default }) },
          { path: 'activities/:id', lazy: async () => ({ Component: (await import('@/pages/admin/ActivityPage')).default }) },
          { path: 'orders', lazy: async () => ({ Component: (await import('@/pages/admin/OrdersPage')).default }) },
          { path: 'orders/:id', lazy: async () => ({ Component: (await import('@/pages/admin/OrderPage')).default }) },
          { path: 'outbox', lazy: async () => ({ Component: (await import('@/pages/admin/OutboxPage')).default }) },
          { path: 'outbox/:id', lazy: async () => ({ Component: (await import('@/pages/admin/OutboxDetailPage')).default }) },
          { path: '*', lazy: async () => ({ Component: (await import('@/pages/NotFoundPage')).default }) },
        ],
      }],
    },
    {
      element: <AppLayout><Outlet /></AppLayout>,
      children: [
        { path: '/', lazy: async () => ({ Component: (await import('@/pages/HomePage')).default }) },
        {
          element: <RequireAuth />,
          children: [
            { path: '/products', lazy: async () => ({ Component: (await import('@/pages/ProductListPage')).default }) },
            { path: '/products/:id', lazy: async () => ({ Component: (await import('@/pages/ProductDetailPage')).default }) },
            { path: '/orders', lazy: async () => ({ Component: (await import('@/pages/OrderListPage')).default }) },
            { path: '/orders/:id', lazy: async () => ({ Component: (await import('@/pages/OrderDetailPage')).default }) },
            { path: '/seckill', lazy: async () => ({ Component: (await import('@/pages/SeckillListPage')).default }) },
            { path: '/seckill/orders', lazy: async () => ({ Component: (await import('@/pages/SeckillOrderPage')).default }) },
            { path: '/seckill/orders/:orderId', lazy: async () => ({ Component: (await import('@/pages/SeckillOrderPage')).default }) },
            { path: '/seckill/:id', lazy: async () => ({ Component: (await import('@/pages/SeckillDetailPage')).default }) },
            { path: '/recommend', lazy: async () => ({ Component: (await import('@/pages/RecommendPage')).default }) },
            { path: '/recommend/:conversationId', lazy: async () => ({ Component: (await import('@/pages/RecommendPage')).default }) },
            { path: '/profile', lazy: async () => ({ Component: (await import('@/pages/ProfilePage')).default }) },
          ],
        },
        { path: '*', lazy: async () => ({ Component: (await import('@/pages/NotFoundPage')).default }) },
      ],
    },
    {
      element: <GuestOnly />,
      children: [
        { path: '/login', lazy: async () => ({ Component: (await import('@/pages/LoginPage')).default }) },
        { path: '/register', lazy: async () => ({ Component: (await import('@/pages/RegisterPage')).default }) },
      ],
    },
  ],
}])

export default router
