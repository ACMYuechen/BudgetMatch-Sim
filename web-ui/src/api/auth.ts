import request from './request'
import type { LoginResp, RegisterReq, RegisterResp } from '@/types/api'

export function loginByUsername(username: string, password: string) {
  return request.post<LoginResp>('/auth/login/username', { username, password })
}

export function loginByEmail(email: string, password: string) {
  return request.post<LoginResp>('/auth/login/email', { email, password })
}

export function sendCode(email: string) {
  return request.post<{ success: boolean }>('/auth/code/send', { email })
}

export function register(data: RegisterReq) {
  return request.post<RegisterResp>('/auth/register', data)
}
