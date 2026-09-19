import { expect, test, type Page } from '@playwright/test'
import type { AgentConversationSummary, AgentConversationTurn, AgentRecommendResp } from '../src/types/api'

const user = { id: 'user-demo', username: '小林', email: 'lin@example.com', avatar: '', phone: '', role: 100, status: 1, remark: '' }
const intent = { budget_cents: 40000, max_items: 3, keywords: ['通勤'], preferences: ['轻便'] }
const result: AgentRecommendResp = {
  conversation_id: 'conversation-a', conversation_title: '通勤购物计划', turn_id: 'turn-a', intent,
  summary: '为你挑选了一套轻便的通勤组合，兼顾实用和预算。', total_price_cents: 24800,
  items: [
    { id: 'sku-1', name: '轻行耳机 苔绿版', category: '数码', source: 'mall+rag', price_cents: 19900, stock: 8, score: 1.2, reason: '轻巧便携，满足日常通勤需要。' },
    { id: 'mock-notebook', name: '便携笔记本套装', category: '文具', source: 'mock', price_cents: 4900, stock: 12, score: 0.8, reason: '方便随手记录。' },
  ],
  tools_used: [{ name: 'mall.product_provider', success: true, detail: '检索在售商品与规格' }],
}
const conversation: AgentConversationSummary = { conversation_id: result.conversation_id, conversation_title: result.conversation_title, state: intent, turn_count: 1, created_at_ms: 1789516800000, updated_at_ms: 1789516800000 }
const turn: AgentConversationTurn = { turn_id: result.turn_id, sequence: 1, query: '想要轻便的通勤用品', budget_cents: 40000, max_items: 3, intent, result, created_at_ms: conversation.created_at_ms, completed_at_ms: conversation.updated_at_ms }
const product = { id: 'product-1', user_id: 'seller-1', name: '轻行耳机', content: '轻巧便携', image: '', providor: '轻行数码', status: 1, agent_comment: '', created_at: '', updated_at: '' }
const sku = { id: 'sku-1', product_id: product.id, name: '苔绿版', specs: '{"颜色":"苔绿"}', price: 22900, stock: 6, status: 1, sold: 1, agent_comment: '', created_at: '', updated_at: '' }
const routeA = `/recommend/${conversation.conversation_id}`
const sse = (name: string, data: unknown) => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`
const streamPattern = '**/api/agent/recommend/stream'
const historyPattern = '**/api/agent/conversations/conversation-a/turns?**'

interface LiveStreamControls {
  requests: Record<string, unknown>[]
  accept: string | null
  aborted: boolean
  send: (kind: string, payload: Record<string, unknown>) => void
  finish: (stage: 'final' | 'done' | 'close') => void
}

async function installLiveStream(page: Page) {
  await page.addInitScript((template) => {
    const scope = window as unknown as { streamTest: LiveStreamControls }
    const original = window.fetch.bind(window)
    const requests: Record<string, unknown>[] = []
    window.fetch = async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input)
      if (!url.endsWith('/agent/recommend/stream')) return original(input, init)
      const request = JSON.parse(String(init?.body)) as Record<string, unknown>
      requests.push(request)
      let sequence = 0
      let producer!: ReadableStreamDefaultController<Uint8Array>
      const controls: LiveStreamControls = {
        requests, accept: new Headers(init?.headers).get('Accept'), aborted: false,
        send(kind, payload) {
          sequence++
          const data = { schema_version: 1, execution_id: `execution-${requests.length}`, conversation_id: request.conversation_id, turn_id: request.turn_id, sequence, event: kind, ...payload }
          producer.enqueue(new TextEncoder().encode(`id: ${data.execution_id}:${sequence}\nevent: ${kind}\ndata: ${JSON.stringify(data)}\n\n`))
        },
        finish(stage) {
          if (stage === 'final') controls.send('recommendation.final', { final: { ...template, conversation_id: request.conversation_id, turn_id: request.turn_id } })
          if (stage === 'done') controls.send('done', { done: { ok: true, replayed: false } })
          if (stage === 'close') producer.close()
        },
      }
      scope.streamTest = controls
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          producer = controller
          controls.send('request.accepted', { accepted: {} })
          controls.send('tool.started', { tool: { call_id: 'tool-1', name: 'tool.search_products', status: 'running', duration_ms: 0 } })
          controls.send('tool.completed', { tool: { call_id: 'tool-1', name: 'tool.search_products', status: 'succeeded', duration_ms: 12 } })
          controls.send('answer.delta', { answer_delta: { text: '临时解释 <img src=x onerror=alert(1)>', provisional: true } })
          init?.signal?.addEventListener('abort', () => { controls.aborted = true; try { controller.error(new DOMException('aborted', 'AbortError')) } catch { /* already closed */ } }, { once: true })
        },
        cancel() { controls.aborted = true },
      })
      return new Response(body, { headers: { 'Content-Type': 'text/event-stream; charset=utf-8; version=1' } })
    }
  }, result)
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript((profile) => {
    localStorage.setItem('budgetmatch:token', JSON.stringify('test-token'))
    localStorage.setItem('budgetmatch:userInfo', JSON.stringify(profile))
  }, user)
  const summaries = new Map([[conversation.conversation_id, structuredClone(conversation)]])
  const histories = new Map([[conversation.conversation_id, [structuredClone(turn)]]])
  await page.route('http://127.0.0.1:4173/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/agent/conversations') {
      await route.fulfill({ json: { list: [...summaries.values()], total: summaries.size, page: 1, page_size: 100 } })
    } else if (/\/api\/agent\/conversations\/[^/]+\/turns$/.test(path)) {
      const id = decodeURIComponent(path.split('/')[4])
      if (!summaries.has(id)) { await route.fulfill({ status: 404, json: { message: '会话不存在' } }); return }
      const turns = histories.get(id) || []
      await route.fulfill({ json: { conversation: summaries.get(id), list: turns, total: turns.length, page: 1, page_size: 100 } })
    } else if (path.startsWith('/api/agent/conversations/') && route.request().method() === 'DELETE') {
      const id = decodeURIComponent(path.split('/')[4])
      summaries.delete(id)
      histories.delete(id)
      await route.fulfill({ json: { deleted: true } })
    } else if (path === '/api/agent/recommend/stream') {
      expect(route.request().headers().authorization).toBe('Bearer test-token')
      const body = route.request().postDataJSON()
      expect(body.conversation_id).toBeTruthy()
      expect(body.turn_id).toBeTruthy()
      const previous = histories.get(body.conversation_id) || []
      const nextIntent = { ...intent, budget_cents: body.budget_cents || summaries.get(body.conversation_id)?.state.budget_cents || 40000, max_items: body.max_items || summaries.get(body.conversation_id)?.state.max_items || 3 }
      const final = { ...result, conversation_id: body.conversation_id, turn_id: body.turn_id, intent: nextIntent }
      summaries.set(body.conversation_id, { ...conversation, conversation_id: body.conversation_id, turn_count: previous.length + 1, state: nextIntent })
      histories.set(body.conversation_id, [...previous, { ...turn, turn_id: body.turn_id, query: body.query, sequence: previous.length + 1, intent: nextIntent, result: final }])
      await route.fulfill({ contentType: 'text/event-stream', body: sse('request.accepted', {}) + sse('rpc.started', {}) + sse('recommendation.final', final) + sse('done', { ok: true }) })
    } else if (path === '/api/mall/skus/sku-1') await route.fulfill({ json: { sku } })
    else if (path === '/api/mall/products/product-1') await route.fulfill({ json: { product } })
    else if (path === '/api/mall/skus') await route.fulfill({ json: { list: [sku], total: 1, page: 1, page_size: 100 } })
    else if (path === '/api/mall/orders' && route.request().method() === 'POST') await route.fulfill({ json: { order_id: 'order-from-recommendation', status: 1 } })
    else await route.fulfill({ status: 404, json: { message: '测试未提供此接口' } })
  })
})

async function send(page: Page, query = '预算内选择轻便通勤用品') {
  await page.getByRole('textbox', { name: '购物需求' }).fill(query)
  await page.getByRole('button', { name: '发送需求' }).click()
}

test('新需求只有主动发送才调用推荐，成功后展示预算并进入会话', async ({ page }) => {
  const requests: Record<string, unknown>[] = []
  page.on('request', (request) => { if (request.url().endsWith('/recommend/stream')) requests.push(request.postDataJSON()) })
  await page.goto('/recommend?query=通勤耳机&budget_cents=40000')
  await expect(page.getByRole('textbox', { name: '购物需求' })).toHaveValue('通勤耳机')
  expect(requests).toHaveLength(0)
  await page.getByRole('button', { name: '发送需求' }).click()
  await expect(page.getByText(result.summary)).toBeVisible()
  expect(requests).toHaveLength(1)
  await expect(page).toHaveURL(`/recommend/${requests[0].conversation_id}`)
  expect(requests[0].budget_cents).toBe(40000)
  await expect(page.getByLabel('方案预算概览')).toContainText('¥400.00')
  await expect(page.getByLabel('方案预算概览')).toContainText('¥248.00')
  await expect(page.getByLabel('方案预算概览')).toContainText('¥152.00')
  await expect(page.getByRole('textbox', { name: '购物需求' })).toHaveValue('')
})

test('连续追问继承约束，新的追问使用新的轮次 ID', async ({ page }) => {
  const requests: Record<string, unknown>[] = []
  page.on('request', (request) => { if (request.url().endsWith('/recommend/stream')) requests.push(request.postDataJSON()) })
  await page.goto(routeA)
  await expect(page.getByText(result.summary)).toBeVisible()
  await send(page, '预算不变，选更轻的')
  await expect(page.getByText('你的需求 · 第 2 轮')).toBeVisible()
  await send(page, '再换一个颜色')
  await expect(page.getByText('你的需求 · 第 3 轮')).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[0]).not.toHaveProperty('budget_cents')
  expect(requests[0]).not.toHaveProperty('max_items')
  expect(requests[0].conversation_id).toBe(conversation.conversation_id)
  expect(requests[0].turn_id).not.toBe(requests[1].turn_id)
})

test('失败保留输入，重试原请求保持会话与轮次 ID', async ({ page }) => {
  const requests: unknown[] = []
  await page.route(streamPattern, async (route) => {
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) await route.fulfill({ contentType: 'text/event-stream', body: sse('error', { message: '推荐服务暂忙' }) })
    else await route.fallback()
  })
  await page.goto('/recommend')
  await send(page, '想买通勤耳机')
  await expect(page.getByRole('alert')).toContainText('推荐服务暂忙')
  await expect(page.getByRole('textbox', { name: '购物需求' })).toHaveValue('想买通勤耳机')
  await page.getByRole('button', { name: '重试原请求' }).click()
  await expect(page.getByText(result.summary)).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
})

test('停止生成后可重试，同一请求的迟到响应不会覆盖新状态', async ({ page }) => {
  let release: (() => void) | undefined
  const requests: unknown[] = []
  await page.route(streamPattern, async (route) => {
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) {
      await new Promise<void>((resolve) => { release = resolve })
      await route.fulfill({ contentType: 'text/event-stream', body: sse('error', { message: '迟到的失败' }) })
    } else await route.fallback()
  })
  await page.goto('/recommend')
  await send(page)
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole('button', { name: '停止生成' }).click()
  await expect(page.getByRole('alert')).toContainText('已停止等待')
  expect(requests).toHaveLength(1)
  await page.getByRole('button', { name: '重试原请求' }).click()
  release?.()
  await expect(page.getByText(result.summary)).toBeVisible()
  await expect(page.getByText('迟到的失败')).toHaveCount(0)
  expect(requests[1]).toEqual(requests[0])
})

test('新对话按钮清理未发送草稿，未完成请求不会跳回旧会话', async ({ page }) => {
  let release: (() => void) | undefined
  let responded = false
  await page.route(streamPattern, async (route) => {
    const body = route.request().postDataJSON()
    await new Promise<void>((resolve) => { release = resolve })
    await route.fulfill({ contentType: 'text/event-stream', body: sse('recommendation.final', { ...result, conversation_id: body.conversation_id, turn_id: body.turn_id }) + sse('done', { ok: true }) })
    responded = true
  })
  await page.goto('/recommend')
  await send(page)
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole('button', { name: '新对话', exact: true }).click()
  release?.()
  await expect.poll(() => responded).toBe(true)
  await expect(page).toHaveURL('/recommend')
  await expect(page.getByRole('textbox', { name: '购物需求' })).toHaveValue('')
  await expect(page.getByRole('button', { name: '发送需求' })).toBeEnabled()
  await expect(page.getByText(result.summary)).toHaveCount(0)
})

test('历史加载失败留在原地址，可重试恢复', async ({ page }) => {
  let failure = true
  await page.route(historyPattern, (route) => failure ? route.fulfill({ status: 503, json: { message: '历史服务暂不可用' } }) : route.fallback())
  await page.goto(routeA)
  await expect(page.getByText('会话历史加载失败')).toBeVisible()
  await expect(page).toHaveURL(routeA)
  await expect(page.getByRole('button', { name: '发送需求' })).toBeDisabled()
  failure = false
  await page.getByRole('button', { name: '重试历史' }).click()
  await expect(page.getByText(result.summary)).toBeVisible()
  await expect(page.getByRole('button', { name: '发送需求' })).toBeEnabled()
})

test('摘要及轮次分页全部恢复，按轮次排序', async ({ page }) => {
  const pages: string[] = []
  await page.route('**/api/agent/conversations?**', (route) => {
    const page = new URL(route.request().url()).searchParams.get('page')!
    pages.push(`list-${page}`)
    return route.fulfill({ json: { list: [{ ...conversation, conversation_id: page === '1' ? 'conversation-a' : 'conversation-b', conversation_title: page === '1' ? '通勤购物计划' : '宿舍购物计划' }], total: 2, page: Number(page), page_size: 1 } })
  })
  await page.route(historyPattern, (route) => {
    const page = new URL(route.request().url()).searchParams.get('page')!
    pages.push(`turn-${page}`)
    return route.fulfill({ json: { conversation, list: [{ ...turn, turn_id: `turn-${page}`, sequence: Number(page), query: `第${page}次购物需求` }], total: 2, page: Number(page), page_size: 1 } })
  })
  await page.goto(routeA)
  await expect(page.getByRole('button', { name: /^宿舍购物计划 1 轮/ })).toBeVisible()
  await expect(page.getByText('第2次购物需求')).toBeVisible()
  expect(pages).toContain('list-2')
  expect(pages).toContain('turn-2')
  await expect(page.locator('.recommend-user-message p')).toHaveText(['第1次购物需求', '第2次购物需求'])
})

test('会话切换后旧历史响应不会覆盖当前会话', async ({ page }) => {
  let release: (() => void) | undefined
  let responded = false
  await page.route(historyPattern, async (route) => {
    await new Promise<void>((resolve) => { release = resolve })
    await route.fulfill({ json: { conversation, list: [turn], total: 1, page: 1, page_size: 100 } })
    responded = true
  })
  await page.goto(routeA)
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole('button', { name: '新对话', exact: true }).click()
  release?.()
  await expect.poll(() => responded).toBe(true)
  await expect(page.getByRole('heading', { name: '想买什么？慢慢说。' })).toBeVisible()
  await expect(page.getByText(turn.query, { exact: true })).toHaveCount(0)
})

test('删除会话需确认，失败不移除记录，成功返回新对话', async ({ page }) => {
  let calls = 0
  await page.route('**/api/agent/conversations/conversation-a', (route) => {
    calls++
    return calls === 1 ? route.fulfill({ json: { deleted: false } }) : route.fallback()
  })
  await page.goto(routeA)
  await page.getByRole('button', { name: `删除会话 ${conversation.conversation_title}` }).click()
  const dialog = page.getByRole('dialog', { name: '删除这个会话？' })
  expect(calls).toBe(0)
  await dialog.getByRole('button', { name: '确认删除' }).click()
  await expect(dialog.getByRole('alert')).toContainText('会话尚未删除')
  await dialog.getByRole('button', { name: '确认删除' }).click()
  await expect(page).toHaveURL('/recommend')
  await expect(page.getByText('还没有历史会话')).toBeVisible()
})

test('推荐 SKU 正确定位商品、提示新价格，确认后才创建订单', async ({ page }) => {
  let writes = 0
  let lookup = ''
  page.on('request', (request) => {
    if (request.url().includes('/api/mall/skus/')) lookup = new URL(request.url()).pathname
    if (request.method() === 'POST' && request.url().endsWith('/api/mall/orders')) writes++
  })
  await page.goto(routeA)
  await page.getByRole('button', { name: '查看并购买' }).click()
  await expect(page).toHaveURL('/products/product-1?sku=sku-1')
  expect(lookup).toBe('/api/mall/skus/sku-1')
  await expect(page.getByRole('alert')).toContainText('推荐时为 ¥199.00，当前为 ¥229.00')
  await expect(page.getByRole('radio', { name: /苔绿版/ })).toBeChecked()
  expect(writes).toBe(0)
  await page.getByRole('button', { name: '确认购买' }).click()
  const dialog = page.getByRole('dialog', { name: '确认订单' })
  await expect(dialog).toContainText('¥229.00')
  const request = page.waitForRequest('**/api/mall/orders')
  await dialog.getByRole('button', { name: '提交订单' }).click()
  expect((await request).postDataJSON()).toMatchObject({ sku_id: 'sku-1', quantity: 1 })
  await expect(page).toHaveURL('/orders/order-from-recommendation')
})

test('指定的推荐规格不可售时，不自动替换为另一规格', async ({ page }) => {
  await page.route('**/api/mall/skus?**', (route) => route.fulfill({ json: { list: [{ ...sku, id: 'sku-other', name: '云白版' }], total: 1, page: 1, page_size: 100 } }))
  await page.goto(routeA)
  await page.getByRole('button', { name: '查看并购买' }).click()
  await expect(page.getByText('指定规格已下架或缺货')).toBeVisible()
  await expect(page.getByRole('radio', { name: /云白版/ })).not.toBeChecked()
  await expect(page.getByRole('button', { name: '确认购买' })).toHaveCount(0)
  await page.getByRole('link', { name: '← 返回预算推荐' }).click()
  await expect(page).toHaveURL(routeA)
})

test('示例商品不能直接购买，SKU 查询失败可重试', async ({ page }) => {
  let failure = true
  await page.route('**/api/mall/skus/sku-1', (route) => failure ? route.fulfill({ status: 503, json: { message: '规格查询暂不可用' } }) : route.fallback())
  await page.goto(routeA)
  const mock = page.getByRole('article').filter({ has: page.getByRole('heading', { name: '便携笔记本套装' }) }).last()
  await expect(mock.getByText('示例数据，不能直接购买')).toBeVisible()
  await expect(mock.getByRole('button', { name: '查看并购买' })).toHaveCount(0)
  await page.getByRole('button', { name: '查看并购买' }).click()
  await expect(page.getByRole('alert')).toContainText('规格查询暂不可用')
  failure = false
  await page.getByRole('button', { name: '重试查看商品' }).click()
  await expect(page).toHaveURL('/products/product-1?sku=sku-1')
})

test('已收到最终结果时，随后历史失败也保留方案', async ({ page }) => {
  await page.route('**/api/agent/conversations/*/turns?**', (route) => route.fulfill({ status: 503, json: { message: '历史读取失败' } }))
  await page.goto('/recommend')
  await send(page)
  await expect(page.getByText('会话历史加载失败')).toBeVisible()
  await expect(page.getByText(result.summary)).toBeVisible()
  await expect(page.getByRole('button', { name: '发送需求' })).toBeDisabled()
})

test('流缺少最终结果或返回错误身份时，保留输入并提示重试', async ({ page }) => {
  let calls = 0
  await page.route(streamPattern, (route) => {
    calls++
    return route.fulfill({ contentType: 'text/event-stream', body: calls === 1 ? sse('rpc.started', {}) : sse('recommendation.final', result) })
  })
  await page.goto('/recommend')
  await send(page)
  await expect(page.getByRole('alert')).toContainText('没有收到完整方案')
  await page.getByRole('button', { name: '重试原请求' }).click()
  await expect(page.getByRole('alert')).toContainText('与本次请求不匹配')
  await expect(page).toHaveURL('/recommend')
})

test('超预算与明细金额不一致明确提示', async ({ page }) => {
  await page.route(historyPattern, (route) => route.fulfill({ json: { conversation, list: [{ ...turn, result: { ...result, intent: { ...intent, budget_cents: 20000 }, total_price_cents: 25000 } }], total: 1, page: 1, page_size: 100 } }))
  await page.goto(routeA)
  await expect(page.getByText('这份方案超出了预算，可继续追问调整。')).toBeVisible()
  await expect(page.getByText('商品明细与方案合计不一致')).toBeVisible()
  await expect(page.getByLabel('方案预算概览')).toContainText('超出预算')
  await expect(page.getByLabel('方案预算概览')).toContainText('¥50.00')
})

test('会话列表失败可重试，手机端可切换历史', async ({ page }) => {
  let failure = true
  await page.route('**/api/agent/conversations?**', (route) => failure ? route.fulfill({ status: 503, json: { message: '摘要读取失败' } }) : route.fallback())
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/recommend')
  await page.getByRole('button', { name: '会话记录', exact: true }).click()
  const drawer = page.getByRole('dialog', { name: '我的会话' })
  await expect(drawer.getByRole('alert')).toContainText('会话列表加载失败')
  failure = false
  await drawer.getByRole('button', { name: /^重\s*试$/ }).click()
  await drawer.getByRole('button', { name: /通勤购物计划 1 轮/ }).click()
  await expect(drawer).not.toBeVisible()
  await expect(page).toHaveURL(routeA)
  await expect(page.getByText(result.summary)).toBeVisible()
})

test('超时中止等待，保留原请求供重试', async ({ page }) => {
  await page.clock.install()
  let release: (() => void) | undefined
  await page.route(streamPattern, async (route) => {
    await new Promise<void>((resolve) => { release = resolve })
    await route.fulfill({ contentType: 'text/event-stream', body: sse('error', { message: '迟到的错误' }) })
  })
  await page.goto('/recommend')
  await send(page)
  await expect.poll(() => !!release).toBe(true)
  await page.clock.fastForward(35001)
  await expect(page.getByRole('alert')).toContainText('等待推荐超时')
  await expect(page.getByRole('button', { name: '重试原请求' })).toBeEnabled()
  release?.()
})

test('空需求被拦截，空推荐结果仍能继续对话', async ({ page }) => {
  let calls = 0
  await page.route(streamPattern, (route) => {
    calls++
    const body = route.request().postDataJSON()
    return route.fulfill({ contentType: 'text/event-stream', body: sse('recommendation.final', { ...result, items: [], total_price_cents: 0, conversation_id: body.conversation_id, turn_id: body.turn_id }) + sse('done', { ok: true }) })
  })
  await page.goto(routeA)
  await expect(page.getByText(result.summary)).toBeVisible()
  await page.getByRole('button', { name: '发送需求' }).click()
  await expect(page.getByText('请输入你的购物需求')).toBeVisible()
  expect(calls).toBe(0)
  await send(page, '需要一个特殊的商品')
  await expect(page.getByText('暂时没有找到合适的商品，试试调整预算或换个需求描述。')).toBeVisible()
  await expect(page.getByRole('button', { name: '发送需求' })).toBeEnabled()
})

test('v1 临时解释与工具状态实时展示，正常终帧和 EOF 后才出现商品方案', async ({ page }, testInfo) => {
  await installLiveStream(page)
  await page.goto('/recommend')
  await send(page)
  await expect(page.getByLabel('临时解释')).toContainText('最终方案以校验结果为准')
  await expect(page.getByLabel('工具执行状态')).toContainText('检索商品')
  await expect(page.getByLabel('工具执行状态')).toContainText('已完成')
  await expect(page.locator('.recommend-provisional img')).toHaveCount(0)
  const wire = await page.evaluate(() => {
    const controls = (window as unknown as { streamTest: LiveStreamControls }).streamTest
    return { accept: controls.accept, request: controls.requests[0] }
  })
  expect(wire.accept).toBe('text/event-stream')
  expect(wire.request.stream_version).toBe(1)
  expect(wire.request.conversation_id).toBeTruthy()
  expect(wire.request.turn_id).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('stream-provisional.png'), fullPage: true })
  await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.finish('final'))
  await expect(page.getByText(result.summary)).toHaveCount(0)
  await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.finish('done'))
  await expect(page.getByText(result.summary)).toHaveCount(0)
  await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.finish('close'))
  await expect(page.getByText(result.summary)).toBeVisible()
  await expect(page.getByLabel('临时解释')).toHaveCount(0)
  await expect(page.getByLabel('工具执行状态')).toHaveCount(0)
})

test('v1 增量后失败清除临时内容，重试保持原始请求身份', async ({ page }) => {
  await installLiveStream(page)
  await page.goto('/recommend')
  await send(page, '只看本轮请求')
  await expect(page.getByLabel('临时解释')).toBeVisible()
  await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.send('error', { error: { code: 500000, message: '最终核验失败', retryable: false } }))
  await expect(page.getByRole('alert')).toContainText('最终核验失败')
  await expect(page.getByLabel('临时解释')).toHaveCount(0)
  await expect(page.getByLabel('工具执行状态')).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '购物需求' })).toHaveValue('只看本轮请求')
  await page.getByRole('button', { name: '重试原请求' }).click()
  await expect(page.getByLabel('临时解释')).toBeVisible()
  const requests = await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.requests)
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
  await page.getByRole('button', { name: '停止生成' }).click()
})

for (const mode of ['停止', '超时']) {
  test(`v1 ${mode}清除所有临时内容并关闭流`, async ({ page }) => {
    await installLiveStream(page)
    if (mode === '超时') await page.clock.install()
    await page.goto('/recommend')
    await send(page)
    await expect(page.getByLabel('临时解释')).toBeVisible()
    if (mode === '停止') await page.getByRole('button', { name: '停止生成' }).click()
    else await page.clock.fastForward(35001)
    await expect(page.getByLabel('临时解释')).toHaveCount(0)
    await expect(page.getByLabel('工具执行状态')).toHaveCount(0)
    await expect(page.getByText(result.summary)).toHaveCount(0)
    await expect(page.getByRole('button', { name: '重试原请求' })).toBeEnabled()
    expect(await page.evaluate(() => (window as unknown as { streamTest: LiveStreamControls }).streamTest.aborted)).toBe(true)
  })
}

test('旧协议只有 final 没有 done 时也不确认成功', async ({ page }) => {
  await page.route(streamPattern, (route) => {
    const request = route.request().postDataJSON()
    return route.fulfill({ contentType: 'text/event-stream', body: sse('recommendation.final', { ...result, conversation_id: request.conversation_id, turn_id: request.turn_id }) })
  })
  await page.goto('/recommend')
  await send(page)
  await expect(page.getByRole('alert')).toContainText('完成确认')
  await expect(page.getByText(result.summary)).toHaveCount(0)
})

for (const width of [360, 768, 1280]) {
  test(`推荐页在 ${width}px 下可读且无横向溢出`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/recommend')
    await expect(page.getByRole('heading', { name: '想买什么？慢慢说。' })).toBeVisible()
    const budgetWidth = await page.locator('.recommend-composer-controls .ant-input-number-affix-wrapper').evaluate((element) => ({
      input: element.getBoundingClientRect().width,
      available: element.closest('.ant-form-item')!.getBoundingClientRect().width,
    }))
    expect(budgetWidth.input).toBeGreaterThanOrEqual(budgetWidth.available - 2)
    await page.screenshot({ path: testInfo.outputPath('new-conversation.png'), fullPage: true })
    await page.goto(routeA)
    await expect(page.getByText(result.summary)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
    expect(await page.locator('.recommend-transcript').evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(false)
    await page.screenshot({ path: testInfo.outputPath('conversation.png'), fullPage: true })
  })
}
