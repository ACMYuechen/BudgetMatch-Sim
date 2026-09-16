import { expect, test, type Page } from '@playwright/test'

const user = { id: 'user-1', username: '小林', email: 'lin@example.com', avatar: '', phone: '', role: 100, status: 1, remark: '' }
const now = Date.now()
const activity = { id: 'activity-1', title: '春日桌面焕新', description: '挑选一件实用好物，理性参与。', banner_url: '', start_time: now - 3600000, end_time: now + 3600000, status: 1, created_at: now }
const sku = { id: 'flash-sku-1', activity_id: activity.id, title: '便携台灯', subtitle: '柔和光线，轻巧实用', pic: '', original_price: 29900, seckill_price: 19900, stock: 10, sold: 2, status: 1, sort: 0 }
const order = { order_id: 'sord-1', activity_id: activity.id, sku_id: sku.id, quantity: 1, total_amount: 19900, status: 1, created_at: now }

test.beforeEach(async ({ page }) => {
  await page.addInitScript((profile) => {
    localStorage.setItem('budgetmatch:token', JSON.stringify('test-token'))
    localStorage.setItem('budgetmatch:userInfo', JSON.stringify(profile))
  }, user)
  await page.route('http://127.0.0.1:4173/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/seckill/activities') await route.fulfill({ json: { list: [activity], total: 21, page: Number(url.searchParams.get('page') || 1), page_size: Number(url.searchParams.get('page_size') || 10) } })
    else if (url.pathname === '/api/seckill/activities/activity-1') await route.fulfill({ json: { activity } })
    else if (url.pathname === '/api/seckill/skus') await route.fulfill({ json: { list: [sku], total: 1, page: 1, page_size: 100 } })
    else if (url.pathname === '/api/seckill/token') await route.fulfill({ json: { token: 'single-use-token' } })
    else if (url.pathname === '/api/seckill/orders' && route.request().method() === 'POST') await route.fulfill({ json: { order_id: order.order_id, status: 0 } })
    else if (url.pathname === '/api/seckill/orders/sord-1') await route.fulfill({ json: order })
    else await route.fulfill({ status: 404, json: { message: '测试未提供此接口' } })
  })
})

async function confirmParticipation(page: Page) {
  await page.goto('/seckill/activity-1')
  await page.getByRole('button', { name: '确认参与秒杀' }).click()
  await page.getByRole('button', { name: '获取令牌并提交' }).click()
}

test('秒杀使用真实标题和毫秒时间，列表分页与返回地址可恢复', async ({ page }) => {
  await page.goto('/seckill?page=2')
  await expect(page.getByRole('heading', { name: activity.title })).toBeVisible()
  await expect(page.getByText('进行中', { exact: true })).toBeVisible()
  await expect(page.getByText(/开始：/)).not.toContainText('1970')
  await page.getByRole('button', { name: '查看活动' }).click()
  await expect(page.getByText('剩余参考库存 8')).toBeVisible()
  await expect(page.getByText('每人限购 0')).toHaveCount(0)
  await page.getByRole('link', { name: '返回活动列表' }).click()
  await expect(page).toHaveURL('/seckill?page=2')
})

test('倒计时到开始与结束时自动更新，下线活动不开放参与', async ({ page }) => {
  await page.clock.install({ time: now })
  await page.route('**/api/seckill/activities/activity-1', (route) => route.fulfill({ json: { activity: { ...activity, start_time: now + 3000, end_time: now + 6000 } } }))
  await page.goto('/seckill/activity-1')
  await expect(page.getByText('即将开始', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '确认参与秒杀' })).toBeDisabled()
  await page.clock.fastForward(3500)
  await expect(page.getByRole('button', { name: '确认参与秒杀' })).toBeEnabled()
  await page.clock.fastForward(3500)
  await expect(page.getByText('已结束', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '确认参与秒杀' })).toBeDisabled()
  await page.route('**/api/seckill/activities/activity-1', (route) => route.fulfill({ json: { activity: { ...activity, status: 0 } } }))
  await page.reload()
  await expect(page.getByText('已下线', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '确认参与秒杀' })).toBeDisabled()
})

test('规格跨页加载，售罄禁用，数量不可超过库存', async ({ page }) => {
  await page.route('**/api/seckill/skus?**', (route) => {
    const second = new URL(route.request().url()).searchParams.get('page') === '2'
    return route.fulfill({ json: { list: second ? [sku] : [{ ...sku, id: 'sold-out', title: '已售罄规格', sold: 10 }], total: 2, page: second ? 2 : 1, page_size: 1 } })
  })
  await page.goto('/seckill/activity-1')
  await expect(page.getByRole('radio', { name: /已售罄规格/ })).toBeDisabled()
  await expect(page.getByRole('radio', { name: /便携台灯/ })).toBeChecked()
  await page.getByRole('spinbutton', { name: '秒杀数量' }).fill('')
  await expect(page.getByRole('button', { name: '确认参与秒杀' })).toBeDisabled()
  await page.getByRole('spinbutton', { name: '秒杀数量' }).fill('99')
  await page.getByRole('spinbutton', { name: '秒杀数量' }).blur()
  await expect(page.getByRole('spinbutton', { name: '秒杀数量' })).toHaveValue('8')
})

test('确认后才申请令牌，只提交一次，并进入独立秒杀结果页', async ({ page }) => {
  let tokens = 0
  let submissions = 0
  await page.route('**/api/seckill/token', (route) => { tokens++; return route.fulfill({ json: { token: 'one-time' } }) })
  await page.route('**/api/seckill/orders', async (route) => {
    submissions++
    expect(route.request().postDataJSON()).toEqual({ activity_id: activity.id, sku_id: sku.id, quantity: 1, token: 'one-time' })
    await route.fulfill({ json: { order_id: 'sord-1', status: 0 } })
  })
  await page.goto('/seckill/activity-1')
  await page.getByRole('button', { name: '确认参与秒杀' }).click()
  expect(tokens).toBe(0)
  await page.getByRole('button', { name: '获取令牌并提交' }).click()
  await expect(page).toHaveURL('/seckill/orders/sord-1')
  await expect(page.getByText('秒杀成功', { exact: true })).toBeVisible()
  await expect(page.getByText('¥199.00', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /付款|支付/ })).toHaveCount(0)
  expect(tokens).toBe(1)
  expect(submissions).toBe(1)
})

test('受理后暂未落库会继续查询，成功才显示秒杀成功', async ({ page }) => {
  await page.clock.install()
  let reads = 0
  await page.route('**/api/seckill/orders/sord-1', (route) => ++reads === 1 ? route.fulfill({ status: 404, json: { code: 404004, message: '尚未创建' } }) : route.fulfill({ json: order }))
  await confirmParticipation(page)
  await expect(page.getByText(/暂未查到订单/)).toBeVisible()
  await expect(page.getByText('排队中', { exact: true })).toBeVisible()
  await expect(page.getByText('秒杀成功', { exact: true })).toHaveCount(0)
  await page.clock.fastForward(7100)
  await expect(page.getByText('秒杀成功', { exact: true })).toBeVisible()
  await page.clock.fastForward(9000)
  expect(reads).toBe(2)
})

test('查询失败可以恢复，失败和未知订单状态不会当作成功', async ({ page }) => {
  let reads = 0
  await page.route('**/api/seckill/orders/sord-1', (route) => ++reads === 1 ? route.fulfill({ status: 503, json: { message: '查询暂不可用' } }) : route.fulfill({ json: { ...order, status: 2 } }))
  await page.goto('/seckill/orders/sord-1')
  await expect(page.getByText('查询暂不可用')).toBeVisible()
  await page.getByRole('button', { name: '重新查询结果' }).click()
  await expect(page.getByText('秒杀失败', { exact: true })).toBeVisible()
  await page.route('**/api/seckill/orders/sord-1', (route) => route.fulfill({ json: { ...order, status: 99 } }))
  await page.getByRole('button', { name: '重新查询结果' }).click()
  await expect(page.getByText('未知状态（99）')).toBeVisible()
  await expect(page.getByText('秒杀成功', { exact: true })).toHaveCount(0)
})

test('停止查询忽略迟到结果，离开页面后不再轮询', async ({ page }) => {
  await page.clock.install()
  let release: (() => Promise<void>) | undefined
  let reads = 0
  await page.route('**/api/seckill/orders/sord-1', (route) => { reads++; release = () => route.fulfill({ json: order }).catch(() => {}) })
  await page.goto('/seckill/orders/sord-1')
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole('button', { name: '停止自动查询' }).click()
  await release!()
  await expect(page.getByText('秒杀成功', { exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: '返回活动列表' }).click()
  await page.clock.fastForward(120000)
  expect(reads).toBe(1)
})

test('排队自动查询达到上限后停止，可手动开启新一轮', async ({ page }) => {
  await page.clock.install()
  let reads = 0
  await page.route('**/api/seckill/orders/sord-1', (route) => { reads++; return route.fulfill({ json: { ...order, status: 0 } }) })
  await page.goto('/seckill/orders/sord-1')
  await expect(page.getByText('排队中', { exact: true })).toBeVisible()
  for (let index = 1; index < 20; index++) {
    await page.clock.fastForward(7100)
    await expect.poll(() => reads).toBe(index + 1)
    await expect(page.getByRole('status').filter({ hasText: '正在查询结果' })).toHaveCount(0)
  }
  await expect(page.getByRole('button', { name: '重新查询结果' })).toBeVisible()
  await page.clock.fastForward(60000)
  expect(reads).toBe(20)
  await page.getByRole('button', { name: '重新查询结果' }).click()
  await expect.poll(() => reads).toBe(21)
})

test('提交结果不明确时不自动重发，一次性令牌失效不清除登录', async ({ page }) => {
  let submissions = 0
  await page.route('**/api/seckill/orders', (route) => { submissions++; return route.fulfill({ status: 401, json: { code: 401003, message: '秒杀令牌已失效' } }) })
  await confirmParticipation(page)
  await expect(page.getByText('秒杀令牌已失效')).toBeVisible()
  await expect(page.getByText('提交结果不明确，已暂停再次提交')).toBeVisible()
  await expect(page.getByRole('button', { name: '获取令牌并提交' })).toBeDisabled()
  await expect(page).toHaveURL('/seckill/activity-1')
  expect(await page.evaluate(() => localStorage.getItem('budgetmatch:token'))).toBeTruthy()
  expect(submissions).toBe(1)
})

test('申请令牌失败不提交订单，输入保持可重试', async ({ page }) => {
  let posts = 0
  page.on('request', (request) => { if (request.method() === 'POST' && request.url().endsWith('/api/seckill/orders')) posts++ })
  await page.route('**/api/seckill/token', (route) => route.fulfill({ status: 429, json: { message: '操作频繁，请稍后再试' } }))
  await confirmParticipation(page)
  await expect(page.getByText('操作频繁，请稍后再试')).toBeVisible()
  await expect(page.getByRole('button', { name: '获取令牌并提交' })).toBeEnabled()
  expect(posts).toBe(0)
})

test('列表错误可重试，查询入口使用独立结果路由', async ({ page }) => {
  let calls = 0
  await page.route('**/api/seckill/activities?**', (route) => ++calls === 1 ? route.fulfill({ status: 503, json: { message: '活动服务繁忙' } }) : route.fulfill({ json: { list: [], total: 0, page: 1, page_size: 10 } }))
  await page.goto('/seckill')
  await expect(page.getByText('活动服务繁忙')).toBeVisible()
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.getByText('暂无秒杀活动')).toBeVisible()
  await page.getByRole('link', { name: /已有秒杀订单号/ }).click()
  await page.getByLabel('秒杀订单号').fill('sord-1')
  await page.getByRole('button', { name: '查询结果', exact: true }).click()
  await expect(page).toHaveURL('/seckill/orders/sord-1')
})

for (const width of [360, 768, 1280]) {
  test(`秒杀列表、详情与结果在 ${width}px 下无横向溢出`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    for (const [path, file] of [['/seckill', 'list'], ['/seckill/activity-1', 'detail'], ['/seckill/orders/sord-1', 'result']]) {
      await page.goto(path)
      await expect(page.getByText(path.endsWith('sord-1') ? '秒杀成功' : activity.title, { exact: true }).first()).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
      await page.screenshot({ path: testInfo.outputPath(`${file}.png`), fullPage: true, animations: 'disabled' })
    }
  })
}
