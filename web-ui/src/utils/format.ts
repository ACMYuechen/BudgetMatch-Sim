import { v4 as uuidv4 } from 'uuid'

export function formatPrice(cents: number): string {
  if (cents === undefined || cents === null) return '¥0.00'
  return `¥${(cents / 100).toFixed(2)}`
}

export function formatDateTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatDate(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleDateString('zh-CN')
}

export { getOrderStatusText, getOrderStatusColor, OrderStatus } from '@/constants/orderStatus'

export function generateIdempotencyKey(): string {
  return uuidv4()
}
