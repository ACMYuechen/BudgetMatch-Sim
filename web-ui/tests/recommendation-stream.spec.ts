import { expect, test } from '@playwright/test'
import { readRecommendationEvents, RecommendationStreamDecoder } from '../src/utils/recommendationStream'
import { readServerEvents } from '../src/utils/sse'

const ids = { conversation_id: 'c', turn_id: 't' }
const final = { ...ids, conversation_title: '计划', summary: '最终方案', intent: { budget_cents: 1000, max_items: 1, keywords: [], preferences: [] }, total_price_cents: 0, items: [], tools_used: [] }
const frame = (event: string, sequence: number, payload: Record<string, unknown>) => ({ event, id: `e:${sequence}`, data: { schema_version: 1, execution_id: 'e', ...ids, sequence, event, ...payload } })
const text = (event: ReturnType<typeof frame>) => `id: ${event.id}\nevent: ${event.event}\ndata: ${JSON.stringify(event.data)}\n\n`
const frames = () => [
  frame('request.accepted', 1, { accepted: {} }),
  frame('tool.started', 2, { tool: { call_id: 'tool-1', name: 'tool.search_products', status: 'running', duration_ms: 0 } }),
  frame('tool.completed', 3, { tool: { call_id: 'tool-1', name: 'tool.search_products', status: 'succeeded', duration_ms: 1 } }),
  frame('answer.delta', 4, { answer_delta: { text: '临时解释', provisional: true } }),
  frame('recommendation.final', 5, { final }),
  frame('done', 6, { done: { ok: true, replayed: false } }),
]
const body = (source: string) => new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new TextEncoder().encode(source)); c.close() } })

test('v1 增量先到达，但 final + done 仍须等正常 EOF 才发布', async () => {
  let producer!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({ start(c) { producer = c } })
  const events = frames()
  producer.enqueue(new TextEncoder().encode(events.slice(0, 4).map(text).join('')))
  const reader = readRecommendationEvents(stream, ids, true)
  for (let i = 0; i < 4; i++) expect((await reader.next()).value?.event).toBe(events[i].event)
  producer.enqueue(new TextEncoder().encode(events.slice(4).map(text).join('')))
  let complete = false
  const pending = reader.next().then((value) => { complete = true; return value })
  await new Promise((resolve) => setTimeout(resolve, 10))
  expect(complete).toBe(false)
  producer.close()
  expect((await pending).value).toEqual({ event: 'recommendation.final', result: final })
  expect((await reader.next()).done).toBe(true)
  expect(stream.locked).toBe(false)
})

test('v1 重放和未知事件保持序号但不生成工具或临时文本', () => {
  const decoder = new RecommendationStreamDecoder(ids, true)
  expect(decoder.accept(frame('future.event', 1, { private_payload: 'PRIVATE' }))).toBeUndefined()
  expect(decoder.accept(frame('recommendation.final', 2, { final }))).toBeUndefined()
  expect(decoder.accept(frame('done', 3, { done: { ok: true, replayed: true } }))).toBeUndefined()
  expect(decoder.finish()).toEqual(final)
})

for (const mode of ['sequence', 'identity', 'execution', 'version', 'payload', 'event', 'duplicate final', 'early done', 'missing done', 'late event', 'tool mismatch', 'oversized delta', 'nonprovisional', 'wrong replay']) {
  test(`v1 拒绝 ${mode}，不返回最终方案`, () => {
    const input = frames()
    switch (mode) {
      case 'sequence': input[3].data.sequence++; break
      case 'identity': input[3].data.turn_id = 'another'; break
      case 'execution': input[3].data.execution_id = 'another'; break
      case 'version': input[3].data.schema_version = 2; break
      case 'payload': input[3] = frame('answer.delta', 4, { final }); break
      case 'event': input[3].data.event = 'other'; break
      case 'duplicate final': input[5] = frame('recommendation.final', 6, { final }); break
      case 'early done': input[4] = frame('done', 5, { done: { ok: true, replayed: false } }); break
      case 'missing done': input.pop(); break
      case 'late event': input.push(frame('future.event', 7, {})); break
      case 'tool mismatch': input[2] = frame('tool.completed', 3, { tool: { call_id: 'tool-2', name: 'tool.search_products', status: 'succeeded', duration_ms: 1 } }); break
      case 'oversized delta': input[3] = frame('answer.delta', 4, { answer_delta: { text: 'x'.repeat(2049), provisional: true } }); break
      case 'nonprovisional': input[3] = frame('answer.delta', 4, { answer_delta: { text: '文字', provisional: false } }); break
      case 'wrong replay': input[5] = frame('done', 6, { done: { ok: true, replayed: true } }); break
    }
    const decoder = new RecommendationStreamDecoder(ids, true)
    expect(() => { for (const event of input) decoder.accept(event); decoder.finish() }).toThrow()
  })
}

test('旧协议同样要求 final + done，但不重新调用另一条执行链', async () => {
  const valid = `event: recommendation.final\ndata: ${JSON.stringify(final)}\n\nevent: done\ndata: {"ok":true}\n\n`
  const events = []
  for await (const event of readRecommendationEvents(body(valid), ids, false)) events.push(event)
  expect(events).toEqual([{ event: 'recommendation.final', result: final }])
  await expect(async () => { for await (const event of readRecommendationEvents(body(valid.split('event: done')[0]), ids, false)) void event }).rejects.toThrow('完成确认')
})

test('SSE v1 拒绝未终止帧、错误 UTF-8、超大事件并释放 reader', async () => {
  for (const stream of [body('event: done\ndata: {"ok":true}'), body(`data: ${'x'.repeat(256 * 1024)}\n\n`), new ReadableStream<Uint8Array>({ start(c) { c.enqueue(new Uint8Array([0xff])); c.close() } })]) {
    await expect(async () => { for await (const event of readServerEvents(stream, { strict: true })) void event }).rejects.toThrow()
    expect(stream.locked).toBe(false)
  }
})

test('模型错误和累计增量/事件上限均为失败终态', () => {
  const failure = new RecommendationStreamDecoder(ids, true)
  failure.accept(frames()[0])
  expect(() => failure.accept(frame('error', 2, { error: { code: 500000, message: '核验未完成', retryable: false } }))).toThrow('核验未完成')
  const answer = new RecommendationStreamDecoder(ids, true)
  answer.accept(frames()[0])
  expect(() => { for (let i = 2; i <= 10; i++) answer.accept(frame('answer.delta', i, { answer_delta: { text: 'x'.repeat(2048), provisional: true } })) }).toThrow()
  const count = new RecommendationStreamDecoder(ids, true)
  expect(() => { for (let i = 1; i <= 260; i++) count.accept(frame('future.event', i, {})) }).toThrow()
})
