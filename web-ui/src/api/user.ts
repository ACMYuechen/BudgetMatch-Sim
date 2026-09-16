import request from './request'
import type { UserInfo } from '@/types/api'

export async function getUserInfo(signal?: AbortSignal, token?: string) {
  const response = await request.get<{ user: UserInfo }>('/user/info', {
    signal,
    ...(token ? { headers: { Authorization: `Bearer ${token}` } } : {}),
  })
  if (!response.user?.id) throw new Error('暂时无法获取账户信息，请稍后重试')
  return response.user
}
