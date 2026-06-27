import { useCallback, useRef, useState } from 'react'

interface SSEEvent {
  event: string
  data: unknown
  time: string
}

interface UseSSEOptions {
  onEvent?: (event: SSEEvent) => void
}

export function useSSE(options?: UseSSEOptions) {
  const [events, setEvents] = useState<SSEEvent[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const appendEvent = useCallback((event: SSEEvent) => {
    setEvents((prev) => [...prev, event])
    options?.onEvent?.(event)
  }, [options])

  const start = useCallback(
    async (url: string, body: unknown) => {
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller

      setEvents([])
      setLoading(true)
      setError(null)

      try {
        const res = await fetch(url, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(body),
          signal: controller.signal,
        })

        if (!res.body) {
          throw new Error('响应体为空')
        }

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        let eventName = ''
        let eventData = ''

        try {
          for (;;) {
            const { done, value } = await reader.read()
            if (done) break

            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop() || ''

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
                appendEvent({ event: eventName, data: parsed, time: new Date().toLocaleTimeString() })
                eventName = ''
                eventData = ''
              }
            }
          }
        } finally {
          reader.releaseLock()
        }
      } catch (err) {
        if ((err as Error).name !== 'AbortError') {
          setError((err as Error).message || '流式请求失败')
        }
      } finally {
        setLoading(false)
      }
    },
    [appendEvent]
  )

  const stop = useCallback(() => {
    abortRef.current?.abort()
    setLoading(false)
  }, [])

  return { events, loading, error, start, stop }
}
