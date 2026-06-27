import request from './request'
import type { AgentRecommendResp } from '@/types/api'

export function recommend(data: { query: string; budget_cents?: number; max_items?: number }) {
  return request.post<AgentRecommendResp>('/agent/recommend', data)
}

export async function* recommendStream(
  data: { query: string; budget_cents?: number; max_items?: number },
  signal?: AbortSignal
) {
  const res = await fetch(`${import.meta.env.VITE_API_BASE_URL || '/api'}/agent/recommend/stream`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
    signal,
  })

  if (!res.body) {
    throw new Error('响应体为空')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      let eventName = ''
      let eventData = ''

      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventName = line.slice(7).trim()
        } else if (line.startsWith('data: ')) {
          eventData = line.slice(6).trim()
        } else if (line === '' && eventName) {
          let parsed: unknown = eventData
          try {
            parsed = JSON.parse(eventData)
          } catch {
            // keep raw string
          }
          yield { event: eventName, data: parsed }
          eventName = ''
          eventData = ''
        }
      }
    }
  } finally {
    reader.releaseLock()
  }
}
