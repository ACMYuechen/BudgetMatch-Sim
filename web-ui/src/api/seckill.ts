import request from './request'
import type { Activity, ListResp, Order, SeckillSku } from '@/types/api'

export function getActivityList(params: { page: number; page_size: number }) {
  return request.get<ListResp<Activity>>('/seckill/activities', { params })
}

export function getActivityDetail(id: string) {
  return request.get<{ activity: Activity }>(`/seckill/activities/${id}`)
}

export function getSeckillSkuList(params: { activity_id?: string; page?: number; page_size?: number }) {
  return request.get<ListResp<SeckillSku>>('/seckill/skus', { params })
}

export function acquireToken(data: { activity_id: string; sku_id: string }) {
  return request.post<{ token: string }>('/seckill/token', data)
}

export function submitSeckillOrder(data: {
  activity_id: string
  sku_id: string
  quantity: number
  token: string
}) {
  return request.post<{ order_id: string }>('/seckill/orders', data)
}

export function getSeckillOrder(orderId: string) {
  return request.get<{ order: Order }>(`/seckill/orders/${orderId}`)
}
