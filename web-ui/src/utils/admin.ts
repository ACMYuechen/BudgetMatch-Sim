export function isAdmin(role: unknown) { return typeof role === 'number' && Number.isInteger(role) && role >= 1 && role <= 99 }
export function readFilter(value: string | null, allowed: number[], fallback = -1) { return value !== null && allowed.includes(Number(value)) ? Number(value) : fallback }
export const outboxStatuses: Record<number, string> = { 0: '待发送', 1: '处理中', 2: '已发送', 3: '死信' }
export const activityStatuses: Record<number, string> = { 0: '已下线', 1: '已上线', 2: '已预热' }
export const statusOptions = [{ value: -1, label: '全部状态' }, { value: 1, label: '上架' }, { value: 0, label: '下架' }]
export function localDateInput(time: number) {
  const date = new Date(time)
  return new Date(time - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}
