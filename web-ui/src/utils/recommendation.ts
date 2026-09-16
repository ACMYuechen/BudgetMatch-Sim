import type { AgentRecommendResp } from '@/types/api'

export const MAX_BUDGET_YUAN = 1_000_000_000

export function isRecommendation(value: unknown): value is AgentRecommendResp {
  if (!value || typeof value !== 'object') return false
  const result = value as AgentRecommendResp
  const textList = (list: unknown) => list == null || (Array.isArray(list) && list.every((text) => typeof text === 'string'))
  return typeof result.conversation_id === 'string' && !!result.conversation_id &&
    typeof result.turn_id === 'string' && !!result.turn_id && typeof result.summary === 'string' && typeof result.conversation_title === 'string' &&
    !!result.intent && Number.isSafeInteger(result.intent.budget_cents) && result.intent.budget_cents >= 0 &&
    Number.isInteger(result.intent.max_items) && textList(result.intent.keywords) && textList(result.intent.preferences) &&
    Number.isSafeInteger(result.total_price_cents) && result.total_price_cents >= 0 &&
    (result.tools_used == null || (Array.isArray(result.tools_used) && result.tools_used.every((tool) => tool && typeof tool.name === 'string' && typeof tool.detail === 'string'))) &&
    Array.isArray(result.items) && result.items.every((item) => item && typeof item.id === 'string' &&
      typeof item.name === 'string' && typeof item.source === 'string' && typeof item.category === 'string' && typeof item.reason === 'string' &&
      Number.isSafeInteger(item.price_cents) && item.price_cents >= 0 && Number.isFinite(item.stock))
}

export function recommendationError(error: unknown): string {
  if (error instanceof TypeError) return '连接中断，请检查网络后重试'
  return error instanceof Error ? error.message : '暂时无法生成推荐，请稍后重试'
}
