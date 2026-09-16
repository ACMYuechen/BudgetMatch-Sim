import { expect, test } from '@playwright/test'
import { readServerEvents } from '../src/utils/sse'

test('SSE 兼容跨字节中文、CRLF、省略空格、多行 data 和末尾事件', async () => {
  const bytes = new TextEncoder().encode(': heartbeat\r\nevent:rpc.started\r\ndata: {}\r\n\r\nevent: recommendation.final\r\ndata:{"summary":\r\ndata: "中文方案"}\r\n\r\nevent:done\ndata:{"ok":true}')
  let offset = 0
  const body = new ReadableStream<Uint8Array>({ pull(controller) {
    if (offset < bytes.length) controller.enqueue(bytes.slice(offset, ++offset))
    else controller.close()
  } })
  const events = []
  for await (const event of readServerEvents(body)) events.push(event)
  expect(events).toEqual([
    { event: 'rpc.started', data: {} },
    { event: 'recommendation.final', data: { summary: '中文方案' } },
    { event: 'done', data: { ok: true } },
  ])
  expect(body.locked).toBe(false)
})

test('SSE 调用方提前退出时关闭底层流并释放锁', async () => {
  let cancelled = false
  const body = new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(new TextEncoder().encode('event:recommendation.final\ndata:{}\n\n')) },
    cancel() { cancelled = true },
  })
  for await (const event of readServerEvents(body)) { expect(event.event).toBe('recommendation.final'); break }
  expect(cancelled).toBe(true)
  expect(body.locked).toBe(false)
})
