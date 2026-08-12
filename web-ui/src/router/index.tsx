import { createBrowserRouter, Navigate, Outlet, useNavigate } from 'react-router-dom'
import { AppLayout } from '@/components/AppLayout'
import { useAuthStore } from '@/stores/authStore'
import { useEffect } from 'react'
import type { ReactNode } from 'react'

import HomePage from '@/pages/HomePage'
import LoginPage from '@/pages/LoginPage'
import RegisterPage from '@/pages/RegisterPage'
import ProductListPage from '@/pages/ProductListPage'
import ProductDetailPage from '@/pages/ProductDetailPage'
import OrderListPage from '@/pages/OrderListPage'
import OrderDetailPage from '@/pages/OrderDetailPage'
import SeckillListPage from '@/pages/SeckillListPage'
import SeckillDetailPage from '@/pages/SeckillDetailPage'
import RecommendPage from '@/pages/RecommendPage'
import ProfilePage from '@/pages/ProfilePage'

// eslint-disable-next-line react-refresh/only-export-components
function RequireAuth({ children }: { children: ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)
  const navigate = useNavigate()

  useEffect(() => {
    if (!isAuthenticated) {
      navigate('/login', { replace: true })
    }
  }, [isAuthenticated, navigate])

  return isAuthenticated ? <>{children}</> : null
}

// eslint-disable-next-line react-refresh/only-export-components
function GuestOnly({ children }: { children: ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated)
  return isAuthenticated ? <Navigate to="/" replace /> : <>{children}</>
}

const router = createBrowserRouter([
  {
    path: '/',
    element: (
      <AppLayout>
        <Outlet />
      </AppLayout>
    ),
    children: [
      { index: true, element: <HomePage /> },
      { path: 'products', element: <ProductListPage /> },
      { path: 'products/:id', element: <ProductDetailPage /> },
      { path: 'orders', element: <RequireAuth><OrderListPage /></RequireAuth> },
      { path: 'orders/:id', element: <RequireAuth><OrderDetailPage /></RequireAuth> },
      { path: 'seckill', element: <RequireAuth><SeckillListPage /></RequireAuth> },
      { path: 'seckill/:id', element: <RequireAuth><SeckillDetailPage /></RequireAuth> },
			{ path: 'recommend', element: <RequireAuth><RecommendPage /></RequireAuth> },
			{ path: 'recommend/:conversationId', element: <RequireAuth><RecommendPage /></RequireAuth> },
      { path: 'profile', element: <RequireAuth><ProfilePage /></RequireAuth> },
    ],
  },
  {
    path: '/login',
    element: (
      <GuestOnly>
        <LoginPage />
      </GuestOnly>
    ),
  },
  {
    path: '/register',
    element: (
      <GuestOnly>
        <RegisterPage />
      </GuestOnly>
    ),
  },
])

export default router
