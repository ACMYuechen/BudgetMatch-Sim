import request from './request'
import { v4 as uuidv4 } from 'uuid'
import { readRecommendationEvents } from '@/utils/recommendationStream'
import { useAuthStore } from '@/stores/authStore'
import { expireSession } from '@/utils/session'
import type {
  AgentConversationSummary,
  AgentConversationTurnsResp,
  AgentRecommendResp,
  ListResp,
} from '@/types/api'

export interface AgentRecommendReq {
  query: string
  budget_cents?: number
  max_items?: number
  conversation_id?: string
  turn_id?: string
}

/** 发起一次非流式推荐，主要保留给无需过程状态的调用方。 */
export function recommend(data: AgentRecommendReq) {
  return request.post<AgentRecommendResp>('/agent/recommend', data)
}

/** 分页读取当前登录用户的会话摘要。 */
export function listConversations(page = 1, pageSize = 100, signal?: AbortSignal) {
  return request.get<ListResp<AgentConversationSummary>>('/agent/conversations', {
    params: { page, page_size: pageSize },
    signal,
  })
}

/** 分页读取指定会话按时间正序排列的完整轮次。 */
export function listConversationTurns(
  conversationId: string,
  page = 1,
  pageSize = 100,
  signal?: AbortSignal
) {
  return request.get<AgentConversationTurnsResp>(`/agent/conversations/${encodeURIComponent(conversationId)}/turns`, {
    params: { page, page_size: pageSize },
    signal,
  })
}

/** 删除指定会话及其所有轮次，用户归属由后端鉴权上下文决定。 */
export function deleteConversation(conversationId: string, signal?: AbortSignal) {
  return request.delete<{ deleted: boolean }>(`/agent/conversations/${encodeURIComponent(conversationId)}`, { signal })
}

/**
 * 发起带鉴权的 SSE 推荐请求。
 * 解析状态跨网络 chunk 保留；v1 严格校验帧及终态，旧协议仅保留解析兼容。
 */
export async function* recommendStream(
  data: AgentRecommendReq,
  signal?: AbortSignal
) {
  const token = useAuthStore.getState().token
  const input = { ...data, conversation_id: data.conversation_id || uuidv4(), turn_id: data.turn_id || uuidv4(), stream_version: 1 }
  const res = await fetch(`${import.meta.env.VITE_API_BASE_URL || '/api'}/agent/recommend/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(input),
    signal,
  })

  if (!res.ok) {
    if (res.status === 401) {
      expireSession(token)
    }
    let detail = `请求失败 (${res.status})`
    try {
      const payload = await res.json() as { message?: string; msg?: string; error?: string }
      detail = payload.message || payload.msg || payload.error || detail
    } catch {
      // 非 JSON 错误响应使用状态码兜底。
    }
    throw new Error(detail)
  }

  if (!res.body) {
    throw new Error('响应体为空')
  }

  if (!res.headers.get('content-type')?.includes('text/event-stream')) {
    await res.body.cancel()
    throw new Error('服务未返回推荐事件流，请稍后重试')
  }
  const version = /;\s*version\s*=\s*"?([^";\s]+)/i.exec(res.headers.get('content-type') || '')?.[1]
  if (version && version !== '1') {
    await res.body.cancel()
    throw new Error('推荐事件版本不兼容，请稍后重试')
  }
  // An older gateway ignores stream_version and responds with legacy SSE.
  // Consume that SAME attempt; never rerun unary after a partial/error stream.
  yield* readRecommendationEvents(res.body, input, version === '1')
}

/** 全量摘要按最近更新排序，所有分页复用同一个取消信号。 */
export async function getConversationList(signal: AbortSignal) {
  const first = await listConversations(1, 100, signal)
  const items = [...(first.list || [])]
  const size = first.page_size > 0 ? first.page_size : 100
  for (let page = 2; page <= Math.ceil(first.total / size); page++) {
    const next = await listConversations(page, size, signal)
    items.push(...(next.list || []))
    if (!next.list?.length) break
  }
  return [...new Map(items.map((item) => [item.conversation_id, item])).values()].sort((a, b) => b.updated_at_ms - a.updated_at_ms)
}

export async function getConversationHistory(id: string, signal: AbortSignal) {
  const first = await listConversationTurns(id, 1, 100, signal)
  if (first.conversation?.conversation_id !== id) throw new Error('会话信息不匹配，请重试')
  const turns = [...(first.list || [])]
  const size = first.page_size > 0 ? first.page_size : 100
  for (let page = 2; page <= Math.ceil(first.total / size); page++) {
    const next = await listConversationTurns(id, page, size, signal)
    turns.push(...(next.list || []))
    if (!next.list?.length) break
  }
  return { conversation: first.conversation, turns: [...new Map(turns.map((turn) => [turn.turn_id, turn])).values()].sort((a, b) => a.sequence - b.sequence) }
}
