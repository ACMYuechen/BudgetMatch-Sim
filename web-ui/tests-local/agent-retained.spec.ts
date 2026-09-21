import { expect, test, type Page } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { loadLocalAgentConfig, type LocalAgentUser } from './agentConfig'
import type { AgentConversationTurnsResp, AgentRecommendResp } from '../src/types/api'

const selected = loadLocalAgentConfig()

async function login(page: Page, user: LocalAgentUser) {
  await page.getByLabel('用户名', { exact: true }).fill(user.username)
  await page.getByLabel('密码', { exact: true }).fill(user.password)
  const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/auth/login/username')
  await page.getByRole('button', { name: /^登\s*录$/ }).click()
  expect((await response).status()).toBe(200)
  await expect(page.getByRole('button', { name: '账户菜单' })).toContainText(user.username)
}

async function logout(page: Page) {
  await page.getByRole('button', { name: '账户菜单' }).click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(await page.evaluate(() => localStorage.getItem('budgetmatch:token') === null && localStorage.getItem('budgetmatch:userInfo') === null)).toBe(true)
}

// Credentials stay in browser state; neither response headers nor tokens are
// returned to assertions, reports or screenshots. All requests reach real APIs.
async function api(page: Page, resource: string, body?: unknown, anonymous = false) {
  return page.evaluate(async ({ resource, body, anonymous }) => {
    const token = anonymous ? null : JSON.parse(localStorage.getItem('budgetmatch:token') || 'null')
    const response = await fetch(resource, {
      method: body === undefined ? 'GET' : 'POST', redirect: 'error',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const text = await response.text()
    let data: any
    try { data = JSON.parse(text) } catch { data = undefined }
    return { status: response.status, data }
  }, { resource, body, anonymous })
}

test('真实登录、保留历史、页面两轮推荐、刷新恢复及跨用户隔离', async ({ page, context }, testInfo) => {
  const passed: string[] = []
  const unexpected: string[] = []
  const streams: Record<string, unknown>[] = []
  const requests: { method: string; path: string }[] = []
  let created = ''
  let ownerBefore: AgentConversationTurnsResp | undefined
  let writerBefore = 0
  let history: AgentConversationTurnsResp | undefined
  const step = async (name: string, run: () => Promise<void>) => {
    await test.step(name, run)
    passed.push(name)
  }
  await context.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const apiAllowed = (request.method() === 'GET' && (url.pathname === '/api/user/info' || url.pathname === '/api/agent/conversations' || /^\/api\/agent\/conversations\/[A-Za-z0-9_-]+\/turns$/.test(url.pathname))) ||
      (request.method() === 'POST' && ['/api/auth/login/username', '/api/agent/recommend', '/api/agent/recommend/stream'].includes(url.pathname))
    if (url.origin !== selected.origin || (url.pathname.startsWith('/api/') && !apiAllowed)) {
      unexpected.push('outside selected origin or API allowlist')
      await route.abort('blockedbyclient')
      return
    }
    if (url.pathname.startsWith('/api/')) requests.push({ method: request.method(), path: url.pathname })
    if (url.pathname === '/api/agent/recommend/stream') streams.push(request.postDataJSON())
    await route.continue() // no fulfill, response mocks or synthetic sessions
  })
  const seedPath = `/api/agent/conversations/${selected.seedConversation}/turns?page=1&page_size=100`
  try {
    await step('匿名访问被拒绝，错误密码不会创建登录状态', async () => {
      await page.goto(`/recommend/${selected.seedConversation}`)
      await expect(page).toHaveURL(/\/login\?redirect=/)
      expect((await api(page, '/api/agent/conversations', undefined, true)).status).toBe(401)
      await page.getByLabel('用户名', { exact: true }).fill(selected.owner.username)
      await page.getByLabel('密码', { exact: true }).fill(`${selected.owner.password}-wrong`)
      const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/auth/login/username')
      await page.getByRole('button', { name: /^登\s*录$/ }).click()
      expect((await response).status()).toBe(401)
      await expect(page.getByRole('alert')).toBeVisible()
      expect(await page.evaluate(() => localStorage.getItem('budgetmatch:token') === null)).toBe(true)
    })
    await step('真实账号登录并读取已有两轮记录', async () => {
      await login(page, selected.owner)
      await expect(page).toHaveURL(`${selected.origin}/recommend/${selected.seedConversation}`)
      expect((await api(page, '/api/user/info')).data.user.id).toBe(selected.owner.id)
      const response = await api(page, seedPath)
      expect(response.status).toBe(200)
      ownerBefore = response.data
      expect(ownerBefore?.total).toBe(2)
      await expect(page.getByText('你的需求 · 第 1 轮', { exact: true })).toBeVisible()
      await expect(page.getByText('你的需求 · 第 2 轮', { exact: true })).toBeVisible()
      await expect(page.getByLabel('方案预算概览').nth(0)).toContainText('¥600.00')
      await expect(page.getByLabel('方案预算概览').nth(1)).toContainText('¥400.00')
    })
    await step('刷新页面从数据库恢复原始历史', async () => {
      await page.reload()
      await expect(page.getByText('你的需求 · 第 2 轮', { exact: true })).toBeVisible()
      expect((await api(page, seedPath)).data).toEqual(ownerBefore)
      await page.screenshot({ path: testInfo.outputPath('retained-history.png'), fullPage: true })
    })
    await step('退出切换真实账号，旧会话不可见', async () => {
      await logout(page)
      await login(page, selected.writer)
      expect((await api(page, '/api/user/info')).data.user.id).toBe(selected.writer.id)
      await expect(page.getByText('会话历史加载失败', { exact: true })).toBeVisible()
      await expect(page.getByText('你的需求 · 第 1 轮', { exact: true })).toHaveCount(0)
      expect((await api(page, seedPath)).status).toBe(404)
      const list = (await api(page, '/api/agent/conversations?page=1&page_size=100')).data
      expect(list.list.some((item: { conversation_id: string }) => item.conversation_id === selected.seedConversation)).toBe(false)
      writerBefore = list.total
    })
    await step('页面经真实 SSE 完成两轮预算调整并保留新会话', async () => {
      await page.getByRole('button', { name: '新对话', exact: true }).click()
      for (const [index, budget, maxItems, query] of [
        [1, 600, 3, '【本机页面验收】办公桌面推荐'],
        [2, 400, 2, '预算改为 400 元，最多 2 件'],
      ] as const) {
        await page.getByRole('textbox', { name: '购物需求' }).fill(query)
        await page.getByLabel('本轮预算（元）').fill(String(budget))
        await page.getByLabel('最多件数', { exact: true }).fill(String(maxItems))
        const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/agent/recommend/stream')
        await page.getByRole('button', { name: '发送需求' }).click()
        const stream = await response
        expect(stream.status()).toBe(200)
        expect(stream.headers()['content-type']).toContain('version=1')
        await expect(page.getByText(`你的需求 · 第 ${index} 轮`, { exact: true })).toBeVisible()
        await expect(page.getByLabel('方案预算概览').nth(index - 1)).toContainText(`¥${budget}.00`)
      }
      created = new URL(page.url()).pathname.split('/')[2]
      expect(created).toMatch(/^[A-Za-z0-9_-]{1,128}$/)
      expect(created).not.toBe(selected.seedConversation)
      expect(streams).toHaveLength(2)
      expect(streams.every((request) => request.conversation_id === created && request.stream_version === 1)).toBe(true)
      expect(streams[0].turn_id).not.toBe(streams[1].turn_id)
      const result = await api(page, `/api/agent/conversations/${created}/turns?page=1&page_size=100`)
      expect(result.status).toBe(200)
      history = result.data
      expect(history?.total).toBe(2)
      for (const turn of history!.list) {
        const result = turn.result as AgentRecommendResp
        expect(result.items.length).toBeGreaterThan(0)
        expect(result.items.length).toBeLessThanOrEqual(result.intent.max_items)
        expect(result.items.every((item) => item.source === 'mock')).toBe(true)
        expect(result.total_price_cents).toBeLessThanOrEqual(result.intent.budget_cents)
        expect(result.items.reduce((sum, item) => sum + item.price_cents, 0)).toBe(result.total_price_cents)
      }
      await expect(page.getByRole('button', { name: '查看并购买', exact: true })).toHaveCount(0)
    })
    await step('刷新及同 ID 普通接口重放不新增记录', async () => {
      await page.reload()
      await expect(page.getByText('你的需求 · 第 2 轮', { exact: true })).toBeVisible()
      for (const request of streams) {
        const input = { ...request }
        delete input.stream_version
        const replay = await api(page, '/api/agent/recommend', input)
        expect(replay.status).toBe(200)
        expect(replay.data).toEqual(history!.list.find((turn) => turn.turn_id === input.turn_id)!.result)
      }
      expect((await api(page, `/api/agent/conversations/${created}/turns?page=1&page_size=100`)).data).toEqual(history)
      expect((await api(page, '/api/agent/conversations?page=1&page_size=100')).data.total).toBe(writerBefore + 1)
      await page.screenshot({ path: testInfo.outputPath('new-history-after-reload.png'), fullPage: true })
    })
    await step('再次切回原账号，新账号会话被隔离，原记录不变', async () => {
      await logout(page)
      await login(page, selected.owner)
      await expect(page.getByText('会话历史加载失败', { exact: true })).toBeVisible()
      await expect(page.getByText('你的需求 · 第 1 轮', { exact: true })).toHaveCount(0)
      expect((await api(page, `/api/agent/conversations/${created}/turns?page=1&page_size=100`)).status).toBe(404)
      const list = (await api(page, '/api/agent/conversations?page=1&page_size=100')).data
      expect(list.list.some((item: { conversation_id: string }) => item.conversation_id === created)).toBe(false)
      expect((await api(page, seedPath)).data).toEqual(ownerBefore)
      await page.goto(`/recommend/${selected.seedConversation}`)
      await expect(page.getByText('你的需求 · 第 2 轮', { exact: true })).toBeVisible()
    })
    await step('移动布局恢复历史，退出后清除本地会话', async () => {
      await page.setViewportSize({ width: 390, height: 844 })
      await page.reload()
      await expect(page.getByText('你的需求 · 第 2 轮', { exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('mobile-retained-history.png'), fullPage: true })
      await logout(page)
      expect((await api(page, seedPath, undefined, true)).status).toBe(401)
      expect(unexpected).toEqual([])
    })
  } finally {
    // Safe evidence only. No HAR, full requests, headers, tokens or credentials.
    writeFileSync(testInfo.outputPath('acceptance.json'), JSON.stringify({
      status: passed.length === 8 ? 'pass' : 'incomplete', passed_steps: passed,
      created_conversation: created, seed_conversation: selected.seedConversation,
      new_turn_ids: streams.map((input) => input.turn_id), requests,
      blocked_requests: unexpected.length, preserved_records: true,
      data_source: 'real_postgres_and_auth_with_rule_mock_products',
    }, null, 2), { flag: 'wx', mode: 0o600 })
  }
})
