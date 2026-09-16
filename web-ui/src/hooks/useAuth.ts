import { useEffect } from 'react'
import { useAuthStore } from '@/stores/authStore'
import { getUserInfo as fetchUserInfo } from '@/api/user'

export function useAuth() {
  const { token, userInfo, isAuthenticated, setAuth, clearAuth } = useAuthStore()

  useEffect(() => {
    const controller = new AbortController()
    if (token && !userInfo) {
      fetchUserInfo(controller.signal)
        .then((data) => {
          if (!controller.signal.aborted && useAuthStore.getState().token === token) setAuth(token, data)
        })
        .catch(() => {
          // 401 统一由请求层处理；网络故障不清除已有登录状态。
        })
    }
    return () => controller.abort()
  }, [token, userInfo, setAuth])

  return {
    token,
    userInfo,
    isAuthenticated,
    setAuth,
    clearAuth,
  }
}
