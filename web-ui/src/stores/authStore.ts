import { create } from 'zustand'
import { getItem, setItem, removeItem } from '@/utils/storage'
import type { UserInfo } from '@/types/api'

interface AuthState {
  token: string | null
  userInfo: UserInfo | null
  isAuthenticated: boolean
  setAuth: (token: string, userInfo: UserInfo | null) => void
  clearAuth: () => void
}

function getStoredToken() {
  const token = getItem<unknown>('token')
  return typeof token === 'string' && token.trim() ? token : null
}

function getStoredUserInfo() {
  const user = getItem<UserInfo>('userInfo')
  // 忽略旧版本中存下的 { user: ... } 或临时用户结构。
  return user && typeof user.id === 'string' && typeof user.username === 'string' ? user : null
}

const storedToken = getStoredToken()

export const useAuthStore = create<AuthState>()((set) => ({
  token: storedToken,
  userInfo: storedToken ? getStoredUserInfo() : null,
  isAuthenticated: !!storedToken,
  setAuth: (token, userInfo) => {
    setItem('token', token)
    setItem('userInfo', userInfo)
    set({ token, userInfo, isAuthenticated: true })
  },
  clearAuth: () => {
    removeItem('token')
    removeItem('userInfo')
    set({ token: null, userInfo: null, isAuthenticated: false })
  },
}))
