import request from './request'
import { getItem, removeItem } from '@/utils/storage'
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
export function listConversations(page = 1, pageSize = 100) {
  return request.get<ListResp<AgentConversationSummary>>('/agent/conversations', {
    params: { page, page_size: pageSize },
  })
}

/** 分页读取指定会话按时间正序排列的完整轮次。 */
export function listConversationTurns(
  conversationId: string,
  page = 1,
  pageSize = 100,
  signal?: AbortSignal
) {
  return request.get<AgentConversationTurnsResp>(`/agent/conversations/${conversationId}/turns`, {
    params: { page, page_size: pageSize },
    signal,
  })
}

/** 删除指定会话及其所有轮次，用户归属由后端鉴权上下文决定。 */
export function deleteConversation(conversationId: string) {
  return request.delete<{ deleted: boolean }>(`/agent/conversations/${conversationId}`)
}

/**
 * 发起带鉴权的 SSE 推荐请求。
 * 解析状态跨网络 chunk 保留，兼容 CRLF、多行 data 以及末尾没有空行的事件。
 */
export async function* recommendStream(
  data: AgentRecommendReq,
  signal?: AbortSignal
) {
  const token = getItem<string>('token')
  const res = await fetch(`${import.meta.env.VITE_API_BASE_URL || '/api'}/agent/recommend/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(data),
    signal,
  })

  if (!res.ok) {
    if (res.status === 401) {
      removeItem('token')
      removeItem('userInfo')
      window.location.href = '/login'
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

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let eventName = ''
  let eventData: string[] = []

  try {
    // TextDecoder 的 stream 模式避免多字节中文恰好跨 chunk 时产生乱码。
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const rawLine of lines) {
        const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
        if (line.startsWith('event: ')) {
          eventName = line.slice(7).trim()
        } else if (line.startsWith('data: ')) {
          eventData.push(line.slice(6).trim())
        } else if (line === '' && eventName) {
          const joinedData = eventData.join('\n')
          let parsed: unknown = joinedData
          try {
            parsed = JSON.parse(joinedData)
          } catch {
            // keep raw string
          }
          yield { event: eventName, data: parsed }
          eventName = ''
          eventData = []
        }
      }
    }

    // 服务端关闭连接时可能没有发送最后一个空行，因此手动冲刷剩余事件。
    buffer += decoder.decode()
    for (const rawLine of buffer.split('\n')) {
      const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
      if (line.startsWith('event: ')) eventName = line.slice(7).trim()
      else if (line.startsWith('data: ')) eventData.push(line.slice(6).trim())
    }
    if (eventName) {
      const joinedData = eventData.join('\n')
      let parsed: unknown = joinedData
      try {
        parsed = JSON.parse(joinedData)
      } catch {
        // keep raw string
      }
      yield { event: eventName, data: parsed }
    }
  } finally {
    reader.releaseLock()
  }
}
