import type { Activity, SeckillSku } from '@/types/api'

export function activityState(activity: Activity, now: number) {
  if (![1, 2].includes(activity.status)) return { label: activity.status === 0 ? '已下线' : '状态未知', open: false, deadline: 0 }
  if (!Number.isFinite(activity.start_time) || !Number.isFinite(activity.end_time) || activity.end_time <= activity.start_time) return { label: '时间待确认', open: false, deadline: 0 }
  if (now < activity.start_time) return { label: '即将开始', open: false, deadline: activity.start_time }
  if (now >= activity.end_time) return { label: '已结束', open: false, deadline: 0 }
  return { label: '进行中', open: true, deadline: activity.end_time }
}
export function availableStock(sku: SeckillSku) { return Math.max(0, sku.stock - sku.sold) }
export function formatMilliseconds(value: number) {
  return value > 0 && Number.isFinite(value) ? new Date(value).toLocaleString('zh-CN') : '—'
}
export function countdown(deadline: number, now: number) {
  const seconds = Math.max(0, Math.ceil((deadline - now) / 1000))
  return `${Math.floor(seconds / 86400)} 天 ${String(Math.floor(seconds / 3600) % 24).padStart(2, '0')}:${String(Math.floor(seconds / 60) % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}
export const seckillOrderStatus: Record<number, string> = { 0: '排队中', 1: '秒杀成功', 2: '秒杀失败', 3: '已支付', 4: '已关闭' }
