import request from './request'
import type { UpdateUserProfileReq, UserInfo, UserProfile } from '@/types/api'

export function getUserInfo() {
  return request.get<UserInfo>('/user/info')
}

export function getUserProfile() {
  return request.get<UserProfile>('/user/profile')
}

export function updateUserProfile(data: UpdateUserProfileReq) {
  return request.put<void>('/user/profile', data)
}
