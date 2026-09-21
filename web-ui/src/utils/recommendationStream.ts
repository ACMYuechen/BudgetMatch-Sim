import type { AgentRecommendResp } from '@/types/api'
import { isRecommendation } from './recommendation'
import { readServerEvents, type ServerEvent } from './sse'

export interface StreamTool {
  call_id: string
  name: string
  status: 'running' | 'succeeded' | 'failed'
  duration_ms: number
  error_code?: string
}

export type RecommendationEvent =
  | { event: 'request.accepted' | 'rpc.started' }
  | { event: 'answer.delta'; text: string }
  | { event: 'tool.started' | 'tool.completed'; tool: StreamTool }
  | { event: 'recommendation.final'; result: AgentRecommendResp }

const protocolError = () => new Error('推荐事件不完整或顺序异常，请重试原请求')
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const bytes = (text: string) => new TextEncoder().encode(text).length
const toolName = /^tool\.(search_products|select_bundle|read_file|write_file|external_[0-9a-f]{16})$/

/** Stateful v1 validation; legacy uses the same final + done + EOF success gate.
 * Unknown v1 events advance sequence but never provide UI data or an outcome.
 */
export class RecommendationStreamDecoder {
  private sequence = 0
  private execution = ''
  private accepted = false
  private done = false
  private count = 0
  private answerBytes = 0
  private calls = new Map<string, StreamTool>()
  private result?: AgentRecommendResp

  constructor(private ids: { conversation_id: string; turn_id: string }, private versioned: boolean) {}

  accept(frame: ServerEvent): RecommendationEvent | undefined {
    if (++this.count > 259 || this.done) throw protocolError()
    let data = frame.data
    if (this.versioned) {
      if (!object(data) || data.schema_version !== 1 || data.event !== frame.event ||
        data.conversation_id !== this.ids.conversation_id || data.turn_id !== this.ids.turn_id ||
        typeof data.execution_id !== 'string' || !/^[a-zA-Z0-9_-]{1,128}$/.test(data.execution_id) ||
        !Number.isSafeInteger(data.sequence) || data.sequence !== this.sequence + 1 ||
        (this.execution && data.execution_id !== this.execution)) throw protocolError()
      this.execution = data.execution_id
      this.sequence++
      if (frame.id && frame.id !== `${this.execution}:${this.sequence}`) throw protocolError()
      const payloadKey: Record<string, string> = { 'request.accepted': 'accepted', 'answer.delta': 'answer_delta', 'tool.started': 'tool', 'tool.completed': 'tool', 'recommendation.final': 'final', error: 'error', done: 'done' }
      const key = payloadKey[frame.event]
      if (key) {
        const envelope = data
        const present = ['accepted', 'answer_delta', 'tool', 'final', 'error', 'done'].filter((field) => envelope[field] != null)
        if (present.length !== 1 || present[0] !== key) throw protocolError()
        data = envelope[key]
      }
    }
    if (this.result && frame.event !== 'done') throw protocolError()
    switch (frame.event) {
      case 'request.accepted':
        if (this.accepted || !object(data)) throw protocolError()
        this.accepted = true
        return { event: 'request.accepted' }
      case 'rpc.started':
        if (!this.versioned) return { event: 'rpc.started' }
        return // unknown event in additive v1
      case 'answer.delta':
        if (!this.versioned || !this.accepted || !object(data) || data.provisional !== true || typeof data.text !== 'string' || !data.text || bytes(data.text) > 2048) throw protocolError()
        this.answerBytes += bytes(data.text)
        if (this.answerBytes > 16384) throw protocolError()
        return { event: 'answer.delta', text: data.text }
      case 'tool.started':
      case 'tool.completed': {
        if (!this.versioned || !this.accepted || !object(data) || typeof data.call_id !== 'string' || !/^tool-[1-9][0-9]{0,2}$/.test(data.call_id) ||
          typeof data.name !== 'string' || !toolName.test(data.name) || !Number.isSafeInteger(data.duration_ms) ||
          (data.duration_ms as number) < 0 || (data.duration_ms as number) > 30000) throw protocolError()
        const prior = this.calls.get(data.call_id)
        if (frame.event === 'tool.started') {
          if (prior || this.calls.size >= 32 || data.status !== 'running' || data.duration_ms !== 0 || data.error_code) throw protocolError()
        } else if (!prior || prior.status !== 'running' || prior.name !== data.name || !['succeeded', 'failed'].includes(String(data.status)) ||
          (data.error_code != null && (typeof data.error_code !== 'string' || !/^[a-z_]{0,64}$/.test(data.error_code)))) throw protocolError()
        const tool = data as unknown as StreamTool
        this.calls.set(tool.call_id, tool)
        return { event: frame.event, tool }
      }
      case 'recommendation.final':
        if (!isRecommendation(data) || data.conversation_id !== this.ids.conversation_id || data.turn_id !== this.ids.turn_id) {
          throw new Error('推荐结果不完整或与本次请求不匹配，请重试原请求')
        }
        if ([...this.calls.values()].some((call) => call.status === 'running')) throw protocolError()
        this.result = data
        return // Never publish a result before matching done AND normal EOF.
      case 'error':
        if (!object(data) || typeof data.message !== 'string' || !data.message || data.message.length > 1024) throw protocolError()
        throw new Error(data.message)
      case 'done':
        if (!this.result || !object(data) || data.ok !== true || (this.versioned && data.replayed !== !this.accepted)) throw protocolError()
        this.done = true
        return
      default:
        return
    }
  }

  finish(): AgentRecommendResp {
    if (!this.result || !this.done) throw new Error('连接已结束，但没有收到完整方案及完成确认。请重试原请求。')
    return this.result
  }
}

export async function* readRecommendationEvents(body: ReadableStream<Uint8Array>, ids: { conversation_id: string; turn_id: string }, versioned: boolean): AsyncGenerator<RecommendationEvent> {
  const decoder = new RecommendationStreamDecoder(ids, versioned)
  for await (const frame of readServerEvents(body, { strict: versioned })) {
    const event = decoder.accept(frame)
    if (event) yield event
  }
  yield { event: 'recommendation.final', result: decoder.finish() }
}
