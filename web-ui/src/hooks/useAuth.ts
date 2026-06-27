import { useEffect } from 'react'
import { useAuthStore } from '@/stores/authStore'
import { getUserInfo as fetchUserInfo } from '@/api/user'

export function useAuth() {
  const { token, userInfo, isAuthenticated, setAuth, clearAuth } = useAuthStore()

  useEffect(() => {
    if (token && !userInfo) {
      fetchUserInfo()
        .then((data) => {
          setAuth(token, data)
        })
        .catch(() => {
          clearAuth()
        })
    }
  }, [token, userInfo, setAuth, clearAuth])

  return {
    token,
    userInfo,
    isAuthenticated,
    setAuth,
    clearAuth,
  }
}
