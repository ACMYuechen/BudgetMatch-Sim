export interface ServerEvent { event: string; data: unknown }

/** 逐块解析 SSE；支持多字节文本、CRLF、注释、多行 data 和省略字段后的空格。 */
export async function* readServerEvents(body: ReadableStream<Uint8Array>): AsyncGenerator<ServerEvent> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let event = ''
  let data: string[] = []
  const dispatch = (): ServerEvent | null => {
    if (!data.length) { event = ''; return null }
    const joined = data.join('\n')
    let payload: unknown = joined
    try { payload = JSON.parse(joined) } catch { /* 由调用方处理非 JSON 事件。 */ }
    const result = { event: event || 'message', data: payload }
    event = ''
    data = []
    return result
  }
  const consume = (raw: string): ServerEvent | null => {
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw
    if (!line) return dispatch()
    if (line.startsWith(':')) return null
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') event = value
    if (field === 'data') data.push(value)
    return null
  }
  try {
    for (;;) {
      const chunk = await reader.read()
      buffer += chunk.done ? decoder.decode() : decoder.decode(chunk.value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const parsed = consume(line)
        if (parsed) yield parsed
      }
      if (chunk.done) break
    }
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
