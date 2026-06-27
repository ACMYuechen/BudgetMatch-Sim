import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosError } from 'axios'
import { getItem, removeItem } from '@/utils/storage'

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

const instance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

instance.interceptors.request.use((config) => {
  const token = getItem<string>('token')
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

instance.interceptors.response.use(
  (response) => response.data,
  (error: AxiosError) => {
    const status = error.response?.status
    const data = error.response?.data

    console.error('[API Error]', {
      url: error.config?.url,
      method: error.config?.method,
      status,
      data,
    })

    if (status === 401) {
      removeItem('token')
      removeItem('userInfo')
      window.location.href = '/login'
    }

    const backendMsg = extractErrorMessage(data)
    const msg = backendMsg || error.message || `请求失败 (${status})`
    return Promise.reject(new Error(msg))
  }
)

const request = instance as CustomAxiosInstance

export default request
