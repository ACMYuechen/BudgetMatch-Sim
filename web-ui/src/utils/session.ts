import { useAuthStore } from '@/stores/authStore'

/** 旧请求的 401 不应清除用户随后建立的新会话。路由守卫负责返回登录页。 */
export function expireSession(requestToken: string | null) {
  const auth = useAuthStore.getState()
  if (requestToken && auth.token === requestToken) auth.clearAuth()
}
