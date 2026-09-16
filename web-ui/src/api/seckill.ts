import request from './request'
import type { Activity, ListResp, SeckillOrder, SeckillSku } from '@/types/api'

export function getActivityList(params: { page: number; page_size: number; status?: number }, signal?: AbortSignal) {
  return request.get<ListResp<Activity>>('/seckill/activities', { params, signal })
}
export function getActivityDetail(id: string, signal?: AbortSignal) {
  return request.get<{ activity: Activity }>(`/seckill/activities/${encodeURIComponent(id)}`, { signal })
}
export function getSeckillSkuList(params: { activity_id: string; page: number; page_size: number }, signal?: AbortSignal) {
  return request.get<ListResp<SeckillSku>>('/seckill/skus', { params, signal })
}
export async function getActivitySkus(id: string, signal: AbortSignal) {
  const first = await getSeckillSkuList({ activity_id: id, page: 1, page_size: 100 }, signal)
  const items = [...(first.list || [])]
  const size = first.page_size > 0 ? first.page_size : 100
  for (let page = 2; page <= Math.ceil(first.total / size); page++) {
    const next = await getSeckillSkuList({ activity_id: id, page, page_size: size }, signal)
    items.push(...(next.list || []))
    if (!next.list?.length) break
  }
  return [...new Map(items.map((item) => [item.id, item])).values()]
}
export function acquireToken(data: { activity_id: string; sku_id: string }, signal?: AbortSignal) {
  return request.post<{ token: string }>('/seckill/token', data, { signal })
}
export function submitSeckillOrder(data: { activity_id: string; sku_id: string; quantity: number; token: string }, signal?: AbortSignal) {
  return request.post<{ order_id: string; status: number }>('/seckill/orders', data, { signal })
}
export function getSeckillOrder(orderId: string, signal?: AbortSignal) {
  return request.get<SeckillOrder>(`/seckill/orders/${encodeURIComponent(orderId)}`, { signal })
}
