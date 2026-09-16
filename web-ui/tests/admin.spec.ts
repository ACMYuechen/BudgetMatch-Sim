import { expect, test } from '@playwright/test'

const admin = { id: 'admin-1', username: '管理员', email: 'admin@example.com', avatar: '', phone: '', role: 2, status: 1, remark: '' }
const productFixture = { id: 'product-1', user_id: 'seller-1', name: '轻行耳机', content: '轻便实用，适合通勤。', image: '', providor: '轻行数码', status: 1, agent_comment: '推荐轻便款', created_at: '2026-09-16T10:00:00Z', updated_at: '2026-09-16T10:00:00Z' }
const skuFixture = { id: 'sku-1', product_id: 'product-1', name: '苔绿版', specs: '{"颜色":"苔绿"}', price: 19900, stock: 8, sold: 2, status: 1, agent_comment: '', created_at: '', updated_at: '' }
const activityFixture = { id: 'activity-1', title: '桌面焕新活动', description: '精选实用小物', banner_url: '', start_time: 1789603200000, end_time: 1789689600000, status: 0, created_at: 1789516800000 }
const flashFixture = { id: 'flash-1', activity_id: 'activity-1', title: '轻便耳机秒杀', subtitle: '苔绿版', pic: '', original_price: 19900, seckill_price: 9900, stock: 10, sold: 2, lock_stock: 0, status: 1, sort: 0 }
const orderFixture = { id: 'order-1', user_id: 'user-1', original_amount: 19900, discount_amount: 0, pay_amount: 19900, status: 2, pay_type: 'alipay', pay_time: '2026-09-16T10:00:00Z', remark: '请轻放', snapshot: '', idempotency_key: 'key-1', items: [{ product_id: 'product-1', sku_id: 'sku-1', sku_name: '苔绿版', price: 19900, quantity: 1, discount_amount: 0, total_amount: 19900, snapshot: '' }], created_at: '2026-09-16T10:00:00Z', updated_at: '2026-09-16T10:00:00Z', payment_status: 1, out_trade_no: 'pay-1', trade_no: 'ali-1' }
const eventFixture = { id: 'event-1', aggregate_id: 'order-1', event_type: 'paid', dedup_key: 'order:order-1:paid', topic: 'orders', tag: 'paid', message_key: 'order-1', payload: '{"order_id":"order-1","note":"<img src=x onerror=alert(1)>"}', status: 3, attempts: 3, max_attempts: 3, next_retry_at: '', locked_until: '', last_error: '消息队列暂不可用', published_at: 0, created_at: '2026-09-16T10:00:00Z', updated_at: '2026-09-16T10:00:00Z' }

test.beforeEach(async ({ page }) => {
  await page.addInitScript((profile) => {
    localStorage.setItem('budgetmatch:token', JSON.stringify('admin-token'))
    localStorage.setItem('budgetmatch:userInfo', JSON.stringify(profile))
  }, admin)
  let product = structuredClone(productFixture)
  let products = [product]
  let skus = [structuredClone(skuFixture)]
  let activity = structuredClone(activityFixture)
  let activities = [activity]
  let flashes = [structuredClone(flashFixture)]
  let order = structuredClone(orderFixture)
  let event = structuredClone(eventFixture)
  await page.route('http://127.0.0.1:4173/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()
    const list = (items: unknown[], total = items.length) => route.fulfill({ json: { list: items, total, page: Number(url.searchParams.get('page') || 1), page_size: 10 } })
    const ok = () => route.fulfill({ json: { success: true } })
    if (path === '/api/user/info') await route.fulfill({ json: { user: admin } })
    else if (path === '/api/admin/mall/products') {
      if (method === 'POST') { products.push({ ...product, ...route.request().postDataJSON(), id: 'product-new' }); await route.fulfill({ json: { id: 'product-new' } }) }
      else await list(products, 21)
    } else if (path === '/api/admin/mall/products/product-1') {
      if (method === 'PUT') { product = { ...product, ...route.request().postDataJSON() }; products[0] = product; await ok() }
      else if (method === 'DELETE') { products = []; await route.fulfill({ status: 204 }) }
      else await route.fulfill({ json: { product } })
    } else if (path === '/api/admin/mall/skus') {
      if (method === 'POST') { skus.push({ ...skuFixture, ...route.request().postDataJSON(), id: 'sku-new' }); await route.fulfill({ json: { id: 'sku-new' } }) }
      else await list(skus)
    } else if (path === '/api/admin/mall/skus/sku-1') {
      if (method === 'DELETE') { skus = []; await route.fulfill({ status: 204 }) }
      else { skus[0] = { ...skus[0], ...route.request().postDataJSON() }; await ok() }
    } else if (path === '/api/admin/seckill/activities') {
      if (method === 'POST') { activities.push({ ...activity, ...route.request().postDataJSON(), id: 'activity-new' }); await route.fulfill({ json: { id: 'activity-new' } }) }
      else await list(activities)
    } else if (/^\/api\/admin\/seckill\/activities\/activity-1\/(preheat|online|offline)$/.test(path)) {
      activity.status = path.endsWith('preheat') ? 2 : path.endsWith('online') ? 1 : 0
      await ok()
    } else if (path === '/api/admin/seckill/activities/activity-1') {
      if (method === 'PUT') { activity = { ...activity, ...route.request().postDataJSON() }; activities[0] = activity; await ok() }
      else if (method === 'DELETE') { activities = []; await ok() }
      else await route.fulfill({ json: { activity } })
    } else if (path === '/api/admin/seckill/skus') {
      if (method === 'POST') { flashes.push({ ...flashFixture, ...route.request().postDataJSON(), id: 'flash-new' }); await route.fulfill({ json: { id: 'flash-new' } }) }
      else await list(flashes)
    } else if (path === '/api/admin/seckill/skus/flash-1') {
      if (method === 'DELETE') flashes = []
      else flashes[0] = { ...flashes[0], ...route.request().postDataJSON() }
      await ok()
    } else if (path === '/api/admin/mall/orders') await list([order])
    else if (path === '/api/admin/mall/orders/order-1') await route.fulfill({ json: { order } })
    else if (path === '/api/admin/mall/orders/order-1/status') { order = { ...order, status: route.request().postDataJSON().status }; await ok() }
    else if (path === '/api/admin/mall/outbox/stats') await route.fulfill({ json: { counts: [{ status: event.status, event_type: 'paid', count: 1 }], oldest_pending_at: 0 } })
    else if (path === '/api/admin/mall/outbox/events') await list([event])
    else if (path === '/api/admin/mall/outbox/events/event-1') await route.fulfill({ json: { event } })
    else if (path === '/api/admin/mall/outbox/events/event-1/replay') { event = { ...event, status: 0, attempts: 0 }; await ok() }
    else await route.fulfill({ status: 404, json: { message: '测试未提供此接口' } })
  })
})

test('管理路由未登录回跳，普通用户与伪造本地角色不能读取管理接口', async ({ page }) => {
  let adminRequests = 0
  page.on('request', (request) => { if (request.url().includes('/api/admin/')) adminRequests++ })
  await page.route('**/api/user/info', (route) => route.fulfill({ json: { user: { ...admin, role: 100 } } }))
  await page.goto('/admin/products')
  await expect(page.getByText('没有管理权限')).toBeVisible()
  expect(adminRequests).toBe(0)
  await page.addInitScript(() => localStorage.clear())
  await page.goto('/admin/outbox?status=3')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(adminRequests).toBe(0)
})

test('管理员身份读取失败可重试，不提前加载管理数据', async ({ page }) => {
  let calls = 0
  let adminRequests = 0
  page.on('request', (request) => { if (request.url().includes('/api/admin/')) adminRequests++ })
  await page.route('**/api/user/info', (route) => ++calls === 1 ? route.fulfill({ status: 503, json: { message: '账户查询失败' } }) : route.fulfill({ json: { user: admin } }))
  await page.goto('/admin')
  await expect(page.getByText('账户查询失败')).toBeVisible()
  expect(adminRequests).toBe(0)
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.getByRole('link', { name: productFixture.name, exact: true })).toBeVisible()
})

test('商品搜索分页留在 URL，详情返回保留筛选', async ({ page }) => {
  await page.goto('/admin/products?page=2&keyword=耳机&status=1')
  await expect(page.getByRole('searchbox', { name: '管理商品搜索' })).toHaveValue('耳机')
  await page.getByRole('link', { name: productFixture.name, exact: true }).click()
  await expect(page.getByRole('heading', { name: productFixture.name })).toBeVisible()
  await page.getByRole('link', { name: '返回商品管理' }).click()
  await expect(page).toHaveURL('/admin/products?page=2&keyword=耳机&status=1')
  await page.getByRole('searchbox', { name: '管理商品搜索' }).fill('台灯')
  await page.getByRole('searchbox', { name: '管理商品搜索' }).press('Enter')
  await expect(page).toHaveURL('/admin/products?keyword=%E5%8F%B0%E7%81%AF&status=1')
})

test('新建商品校验必填项，失败保留输入，成功后刷新列表', async ({ page }) => {
  let calls = 0
  await page.route('**/api/admin/mall/products', async (route) => {
    if (route.request().method() !== 'POST') { await route.fallback(); return }
    calls++
    if (calls === 1) await route.fulfill({ status: 503, json: { message: '商品保存失败' } })
    else await route.fallback()
  })
  await page.goto('/admin/products')
  await page.getByRole('button', { name: '新建商品' }).click()
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByText('请填写商品名称')).toBeVisible()
  expect(calls).toBe(0)
  await page.getByLabel('商品名称').fill('桌面台灯')
  await page.getByLabel('归属用户 ID').fill('seller-1')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByText('商品保存失败')).toBeVisible()
  await expect(page.getByLabel('商品名称')).toHaveValue('桌面台灯')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('link', { name: '桌面台灯' })).toBeVisible()
  expect(calls).toBe(2)
})

test('编辑商品 success=false 不伪装成功，不能无声清空不支持的字段', async ({ page }) => {
  let calls = 0
  await page.route('**/api/admin/mall/products/product-1', async (route) => {
    if (route.request().method() !== 'PUT') { await route.fallback(); return }
    calls++
    expect(route.request().postDataJSON().status).toBe(1)
    await route.fulfill({ json: { success: false } })
  })
  await page.goto('/admin/products/product-1')
  await page.getByRole('button', { name: '编辑商品' }).click()
  await page.getByLabel('供应商').fill('')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByText('现有接口不支持清空此字段，请保留或替换内容')).toBeVisible()
  expect(calls).toBe(0)
  await page.getByLabel('供应商').fill('新的供应商')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByText('服务未确认操作成功，请刷新核对后重试')).toBeVisible()
  await expect(page.getByRole('dialog')).toBeVisible()
})

test('新增商城规格按分提交价格和产品 ID，库存校验与编辑归零', async ({ page }) => {
  const writes: { url: string; body: Record<string, unknown> }[] = []
  page.on('request', (request) => { if (['POST', 'PUT'].includes(request.method()) && request.url().includes('/mall/skus')) writes.push({ url: request.url(), body: request.postDataJSON() }) })
  await page.goto('/admin/products/product-1')
  await page.getByRole('button', { name: '新增商城规格' }).click()
  await page.getByLabel('规格名称').fill('雾白版')
  await page.getByLabel('价格（分）').fill('12345')
  await page.getByLabel('库存数量').fill('4')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByRole('cell', { name: '雾白版', exact: true })).toBeVisible()
  expect(writes[0].body).toMatchObject({ product_id: 'product-1', name: '雾白版', price: 12345, stock: 4 })
  await page.getByRole('row').filter({ hasText: '苔绿版' }).getByRole('button', { name: '编辑规格', exact: true }).click()
  await page.getByLabel('库存数量').fill('0')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(writes[1].body).toMatchObject({ name: '苔绿版', price: 19900, stock: 0, status: 1 })
})

test('删除商品必须确认，失败不离开详情，成功才回列表', async ({ page }) => {
  let calls = 0
  await page.route('**/api/admin/mall/products/product-1', async (route) => {
    if (route.request().method() !== 'DELETE') { await route.fallback(); return }
    if (++calls === 1) await route.fulfill({ status: 409, json: { message: '商品仍有关联数据' } })
    else await route.fallback()
  })
  await page.goto('/admin/products/product-1')
  await page.getByRole('button', { name: '删除商品', exact: true }).click()
  expect(calls).toBe(0)
  await page.getByRole('button', { name: '确认删除商品' }).click()
  await expect(page.getByText('商品仍有关联数据')).toBeVisible()
  await expect(page).toHaveURL('/admin/products/product-1')
  await page.getByRole('button', { name: '确认删除商品' }).click()
  await expect(page).toHaveURL('/admin/products')
  expect(calls).toBe(2)
})

test('新建活动校验先后时间并发送毫秒时间戳', async ({ page }) => {
  let saved: Record<string, unknown> | undefined
  page.on('request', (request) => { if (request.method() === 'POST' && request.url().endsWith('/seckill/activities')) saved = request.postDataJSON() })
  await page.goto('/admin/activities')
  await page.getByRole('button', { name: '新建活动' }).click()
  await page.getByLabel('活动标题').fill('开学焕新')
  await page.getByLabel('开始时间（本地时区）').fill('2026-10-01T10:00')
  await page.getByLabel('结束时间（本地时区）').fill('2026-10-01T09:00')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByText('结束时间必须晚于开始时间')).toBeVisible()
  expect(saved).toBeUndefined()
  await page.getByLabel('结束时间（本地时区）').fill('2026-10-02T10:00')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByRole('link', { name: '开学焕新' })).toBeVisible()
  expect(Number(saved?.start_time)).toBeGreaterThan(1700000000000)
  expect(Number(saved?.end_time) - Number(saved?.start_time)).toBe(86400000)
})

test('活动预热、上线与下线均确认，活跃状态禁止编辑商品', async ({ page }) => {
  let posts = 0
  page.on('request', (request) => { if (request.method() === 'POST' && request.url().includes('/activities/activity-1/')) posts++ })
  await page.goto('/admin/activities/activity-1')
  await page.getByRole('button', { name: '预热活动', exact: true }).click()
  await expect(page.getByText(/重置 Redis 库存/)).toBeVisible()
  expect(posts).toBe(0)
  await page.getByRole('button', { name: '确认预热活动' }).click()
  await expect(page.getByText('已预热', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '新增秒杀商品' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '预热活动', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '上线活动', exact: true }).click()
  await page.getByRole('button', { name: '确认上线活动' }).click()
  await expect(page.getByText('已上线', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '下线活动', exact: true }).click()
  await page.getByRole('button', { name: '确认下线活动' }).click()
  await expect(page.getByRole('button', { name: '新增秒杀商品' })).toBeEnabled()
  expect(posts).toBe(3)
})

test('秒杀商品编辑保留总库存、状态和价格，不能低于已售数量', async ({ page }) => {
  let saved: Record<string, unknown> | undefined
  page.on('request', (request) => { if (request.method() === 'PUT' && request.url().endsWith('/seckill/skus/flash-1')) saved = request.postDataJSON() })
  await page.goto('/admin/activities/activity-1')
  await page.getByRole('button', { name: '编辑秒杀商品' }).click()
  await page.getByLabel('总库存').fill('1')
  await page.getByLabel('总库存').blur()
  await expect(page.getByLabel('总库存')).toHaveValue('2')
  await page.getByRole('button', { name: /^保\s*存$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(Number(saved?.stock)).toBeGreaterThanOrEqual(2)
  expect(saved).toMatchObject({ status: 1, seckill_price: 9900, original_price: 19900, activity_id: 'activity-1' })
})

test('订单按真实状态与支付流水展示，发货确认后刷新且不能手工付款', async ({ page }) => {
  let posts = 0
  page.on('request', (request) => { if (request.method() === 'PUT' && request.url().endsWith('/orders/order-1/status')) { posts++; expect(request.postDataJSON()).toEqual({ status: 3 }) } })
  await page.goto('/admin/orders?status=2&payment_status=1&user_id=user-1')
  await page.getByRole('link', { name: 'order-1', exact: true }).click()
  await expect(page.getByText('支付成功', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /标记已付款|标记已支付|标记已退款/ })).toHaveCount(0)
  await page.getByRole('button', { name: '标记已发货', exact: true }).click()
  expect(posts).toBe(0)
  await page.getByRole('button', { name: '确认标记已发货' }).click()
  await expect(page.getByText('已发货', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '标记已完成', exact: true })).toBeVisible()
  await page.getByRole('link', { name: '返回订单管理' }).click()
  await expect(page).toHaveURL('/admin/orders?status=2&payment_status=1&user_id=user-1')
})

test('订单状态变更冲突保留原状态，未知状态不给操作入口', async ({ page }) => {
  await page.route('**/api/admin/mall/orders/order-1/status', (route) => route.fulfill({ status: 409, json: { message: '订单状态已变化，请刷新' } }))
  await page.goto('/admin/orders/order-1')
  await page.getByRole('button', { name: '标记已发货', exact: true }).click()
  await page.getByRole('button', { name: '确认标记已发货' }).click()
  await expect(page.getByText('订单状态已变化，请刷新')).toBeVisible()
  await expect(page.getByText('已支付', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '暂不操作' }).click()
  await page.route('**/api/admin/mall/orders/order-1', (route) => route.fulfill({ json: { order: { ...orderFixture, status: 99 } } }))
  await page.getByRole('button', { name: '刷新订单' }).click()
  await expect(page.getByText('未知(99)', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /标记已|取消待支付/ })).toHaveCount(0)
})

test('Outbox 过滤恢复、载荷纯文本，死信重放确认后刷新为待发送', async ({ page }) => {
  let replays = 0
  page.on('request', (request) => { if (request.url().endsWith('/event-1/replay')) replays++ })
  await page.goto('/admin/outbox?status=3&aggregate_id=order-1&event_type=paid')
  await expect(page.getByRole('searchbox', { name: '关联订单 ID' })).toHaveValue('order-1')
  await page.getByRole('link', { name: 'event-1', exact: true }).click()
  await expect(page.locator('.admin-payload')).toContainText('<img src=x onerror=alert(1)>')
  await expect(page.locator('.admin-payload img')).toHaveCount(0)
  await page.getByRole('button', { name: '重放死信', exact: true }).click()
  expect(replays).toBe(0)
  await page.getByRole('button', { name: '确认重放死信' }).click()
  await expect(page.getByText('待发送', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '重放死信', exact: true })).toHaveCount(0)
  expect(replays).toBe(1)
  await page.getByRole('link', { name: '返回消息列表' }).click()
  await expect(page).toHaveURL('/admin/outbox?status=3&aggregate_id=order-1&event_type=paid')
})

test('重放失败不会改变死信状态，单次请求中阻止重复点击', async ({ page }) => {
  let replays = 0
  let release: (() => Promise<void>) | undefined
  await page.route('**/api/admin/mall/outbox/events/event-1/replay', (route) => { replays++; release = () => route.fulfill({ json: { success: false } }) })
  await page.goto('/admin/outbox/event-1')
  await page.getByRole('button', { name: '重放死信', exact: true }).click()
  await page.getByRole('button', { name: '确认重放死信' }).click()
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole('button', { name: '确认重放死信' }).evaluate((element: HTMLButtonElement) => { element.click(); element.click() })
  expect(replays).toBe(1)
  await release!()
  await expect(page.getByText('服务未确认操作成功，请刷新核对后重试')).toBeVisible()
  await expect(page.getByText('死信', { exact: true })).toBeVisible()
})

test('管理列表空状态明确，旧搜索响应不会覆盖新结果', async ({ page }) => {
  let release: (() => Promise<void>) | undefined
  await page.route('**/api/admin/mall/products?**', async (route) => {
    const keyword = new URL(route.request().url()).searchParams.get('keyword')
    if (keyword === '旧商品') release = () => route.fulfill({ json: { list: [{ ...productFixture, name: '迟到商品' }], total: 1, page: 1, page_size: 10 } }).catch(() => {})
    else if (keyword === '新商品') await route.fulfill({ json: { list: [], total: 0, page: 1, page_size: 10 } })
    else await route.fallback()
  })
  await page.goto('/admin/products')
  const search = page.getByRole('searchbox', { name: '管理商品搜索' })
  await search.fill('旧商品')
  await search.press('Enter')
  await expect.poll(() => !!release).toBe(true)
  await search.fill('新商品')
  await search.press('Enter')
  await expect(page.locator('.ant-empty-description')).toHaveText('暂无数据')
  await release!()
  await expect(page.getByRole('link', { name: '迟到商品' })).toHaveCount(0)
})

for (const width of [360, 768, 1280]) {
  test(`管理端四类页面与表单在 ${width}px 下无横向溢出`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    for (const [path, label] of [['products/product-1', '商品规格'], ['activities/activity-1', '秒杀商品'], ['orders/order-1', '订单详情'], ['outbox/event-1', '事件详情']]) {
      await page.goto(`/admin/${path}`)
      await expect(page.getByRole('heading', { name: label, exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
      await page.screenshot({ path: testInfo.outputPath(`${path.split('/')[0]}.png`), fullPage: true, animations: 'disabled' })
    }
    await page.goto('/admin/products')
    await page.getByRole('button', { name: '新建商品' }).click()
    await expect(page.getByLabel('商品名称')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    await page.screenshot({ path: testInfo.outputPath('form.png'), fullPage: true, animations: 'disabled' })
  })
}
