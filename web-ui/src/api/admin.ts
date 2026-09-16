import request from './request'
import type { Activity, ListResp, Product, SeckillSku, Sku } from '@/types/api'
import type { ActivityInput, AdminOrder, OutboxEvent, OutboxStats, ProductInput, SeckillSkuInput, SkuInput } from '@/types/admin'

type Query = Record<string, string | number>
const pathId = (id: string) => encodeURIComponent(id)
async function confirmSuccess(operation: Promise<{ success: boolean }>) {
  if (!(await operation).success) throw new Error('服务未确认操作成功，请刷新核对后重试')
}
async function confirmCreated(operation: Promise<{ id: string }>) {
  if (!(await operation).id) throw new Error('服务未返回记录 ID，请刷新核对是否已创建')
}
export const adminProducts = (params: Query, signal: AbortSignal) => request.get<ListResp<Product>>('/admin/mall/products', { params, signal })
export const adminProduct = (id: string, signal: AbortSignal) => request.get<{ product: Product }>(`/admin/mall/products/${pathId(id)}`, { signal })
export const adminSkus = (params: Query, signal: AbortSignal) => request.get<ListResp<Sku>>('/admin/mall/skus', { params, signal })
export const saveProduct = (id: string | undefined, data: ProductInput, signal: AbortSignal) => id
  ? confirmSuccess(request.put(`/admin/mall/products/${pathId(id)}`, data, { signal }))
  : confirmCreated(request.post('/admin/mall/products', data, { signal }))
export const saveSku = (id: string | undefined, data: SkuInput, signal: AbortSignal) => id
  ? confirmSuccess(request.put(`/admin/mall/skus/${pathId(id)}`, data, { signal }))
  : confirmCreated(request.post('/admin/mall/skus', data, { signal }))
export const deleteProduct = (id: string, signal: AbortSignal) => request.delete<void>(`/admin/mall/products/${pathId(id)}`, { signal })
export const deleteSku = (id: string, signal: AbortSignal) => request.delete<void>(`/admin/mall/skus/${pathId(id)}`, { signal })
export const adminActivities = (params: Query, signal: AbortSignal) => request.get<ListResp<Activity>>('/admin/seckill/activities', { params, signal })
export const adminActivity = (id: string, signal: AbortSignal) => request.get<{ activity: Activity }>(`/admin/seckill/activities/${pathId(id)}`, { signal })
export const adminSeckillSkus = (params: Query, signal: AbortSignal) => request.get<ListResp<SeckillSku>>('/admin/seckill/skus', { params, signal })
export const saveActivity = (id: string | undefined, data: ActivityInput, signal: AbortSignal) => id
  ? confirmSuccess(request.put(`/admin/seckill/activities/${pathId(id)}`, data, { signal }))
  : confirmCreated(request.post('/admin/seckill/activities', data, { signal }))
export function activityAction(id: string, action: 'preheat' | 'online' | 'offline' | 'delete', signal: AbortSignal) {
  const url = `/admin/seckill/activities/${pathId(id)}`
  return confirmSuccess(action === 'delete' ? request.delete(url, { signal }) : request.post(`${url}/${action}`, {}, { signal }))
}
export const saveSeckillSku = (id: string | undefined, data: SeckillSkuInput, signal: AbortSignal) => id
  ? confirmSuccess(request.put(`/admin/seckill/skus/${pathId(id)}`, data, { signal }))
  : confirmCreated(request.post('/admin/seckill/skus', data, { signal }))
export const deleteSeckillSku = (id: string, signal: AbortSignal) => confirmSuccess(request.delete(`/admin/seckill/skus/${pathId(id)}`, { signal }))
export const adminOrders = (params: Query, signal: AbortSignal) => request.get<ListResp<AdminOrder>>('/admin/mall/orders', { params, signal })
export const adminOrder = (id: string, signal: AbortSignal) => request.get<{ order: AdminOrder }>(`/admin/mall/orders/${pathId(id)}`, { signal })
export const updateOrderStatus = (id: string, status: number, signal: AbortSignal) => confirmSuccess(request.put(`/admin/mall/orders/${pathId(id)}/status`, { status }, { signal }))
export const outboxStats = (signal: AbortSignal) => request.get<OutboxStats>('/admin/mall/outbox/stats', { signal })
export const outboxEvents = (params: Query, signal: AbortSignal) => request.get<ListResp<OutboxEvent>>('/admin/mall/outbox/events', { params, signal })
export const outboxEvent = (id: string, signal: AbortSignal) => request.get<{ event: OutboxEvent }>(`/admin/mall/outbox/events/${pathId(id)}`, { signal })
export const replayOutbox = (id: string, signal: AbortSignal) => confirmSuccess(request.post(`/admin/mall/outbox/events/${pathId(id)}/replay`, {}, { signal }))
