import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosError } from 'axios'
import { useAuthStore } from '@/stores/authStore'
import { expireSession } from '@/utils/session'

interface CustomAxiosInstance extends AxiosInstance {
  get<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T>
  post<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T>
  put<T = unknown>(url: string, data?: unknown, config?: AxiosRequestConfig): Promise<T>
  delete<T = unknown>(url: string, config?: AxiosRequestConfig): Promise<T>
}

interface ErrorResponse {
  code?: number
  message?: string
  msg?: string
  error?: string
  data?: unknown
}

function extractErrorMessage(data: unknown): string | null {
  if (typeof data !== 'object' || data === null) return null
  const err = data as ErrorResponse
  return err.message || err.msg || err.error || null
}

export class ApiError extends Error {
  status?: number

  constructor(message: string, status?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const instance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

instance.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token && !config.headers.Authorization && !config.url?.startsWith('/auth/')) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

instance.interceptors.response.use(
  (response) => response.data,
  (error: AxiosError) => {
    if (axios.isCancel(error)) return Promise.reject(error)
    const status = error.response?.status
    const data = error.response?.data

    if (status === 401 && !error.config?.url?.startsWith('/auth/')) {
      const authorization = error.config?.headers?.Authorization
      expireSession(typeof authorization === 'string' ? authorization.replace(/^Bearer\s+/i, '') : null)
    }

    const backendMsg = extractErrorMessage(data)
    const fallback = status === 401 ? '登录信息无效或已过期，请重新登录'
      : error.code === 'ECONNABORTED' ? '请求超时，请稍后重试'
        : !error.response ? '暂时无法连接服务，请检查网络后重试'
          : status && status >= 500 ? '服务暂时不可用，请稍后重试' : '请求失败，请稍后重试'
    return Promise.reject(new ApiError(backendMsg || fallback, status))
  }
)

const request = instance as CustomAxiosInstance

export default request
