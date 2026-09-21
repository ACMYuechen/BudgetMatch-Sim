export interface ServerEvent { event: string; data: unknown; id?: string }

/** 逐块解析 SSE；支持多字节文本、CRLF、注释、多行 data 和省略字段后的空格。 */
export async function* readServerEvents(body: ReadableStream<Uint8Array>, options?: { strict?: boolean }): AsyncGenerator<ServerEvent> {
  const reader = body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  const encoder = new TextEncoder()
  const maxFrame = 256 * 1024
  const maxTotal = 1024 * 1024
  let received = 0
  let frameBytes = 0
  let buffer = ''
  let event = ''
  let id = ''
  let data: string[] = []
  const dispatch = (): ServerEvent | null => {
    frameBytes = 0
    if (!data.length) { event = ''; id = ''; return null }
    const joined = data.join('\n')
    let payload: unknown = joined
    try { payload = JSON.parse(joined) } catch { /* 由调用方处理非 JSON 事件。 */ }
    const result = { event: event || 'message', data: payload, ...(id ? { id } : {}) }
    event = ''
    id = ''
    data = []
    return result
  }
  const consume = (raw: string): ServerEvent | null => {
    frameBytes += encoder.encode(raw).length + 1
    if (frameBytes > maxFrame) throw new Error('推荐事件超过大小限制')
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw
    if (!line) return dispatch()
    if (line.startsWith(':')) return null
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') event = value
    if (field === 'id' && !value.includes('\0')) id = value
    if (field === 'data') data.push(value)
    return null
  }
  try {
    for (;;) {
      const chunk = await reader.read()
      received += chunk.value?.byteLength || 0
      if (received > maxTotal) throw new Error('推荐事件流超过大小限制')
      buffer += chunk.done ? decoder.decode() : decoder.decode(chunk.value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const parsed = consume(line)
        if (parsed) yield parsed
      }
      if (frameBytes + encoder.encode(buffer).length > maxFrame) throw new Error('推荐事件超过大小限制')
      if (chunk.done) break
    }
    if (options?.strict && (buffer || data.length || event || id)) throw new Error('推荐事件被截断，请重试原请求')
    if (buffer) {
      const parsed = consume(buffer)
      if (parsed) yield parsed
    }
    const last = dispatch()
    if (last) yield last
  } finally {
    // 提前拿到最终结果或调用方退出时，真正关闭流，不只释放锁。
    try { await reader.cancel() } catch { /* 已中断的流无需再次清理。 */ }
    reader.releaseLock()
  }
}
