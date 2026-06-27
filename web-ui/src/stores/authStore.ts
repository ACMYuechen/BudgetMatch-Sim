import { create } from 'zustand'
import { getItem, setItem, removeItem } from '@/utils/storage'
import type { UserInfo } from '@/types/api'

interface AuthState {
  token: string | null
  userInfo: UserInfo | null
  isAuthenticated: boolean
  setAuth: (token: string, userInfo: UserInfo) => void
  clearAuth: () => void
}

function getStoredToken() {
  return getItem<string>('token') || null
}

function getStoredUserInfo() {
  return getItem<UserInfo>('userInfo') || null
}

export const useAuthStore = create<AuthState>()((set) => ({
  token: getStoredToken(),
  userInfo: getStoredUserInfo(),
  isAuthenticated: !!getStoredToken(),
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
