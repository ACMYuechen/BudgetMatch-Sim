import { expect, test, type Page } from '@playwright/test'
import type { Order, Product, Sku } from '../src/types/api'

const user = { id: 'user-demo', username: '小林', email: 'lin@example.com', avatar: '', phone: '', role: 100, status: 1, remark: '' }
const product: Product = {
  id: 'product-1', user_id: 'seller-1', name: '轻行无线耳机', content: '轻巧便携，适合每天通勤。\n支持蓝牙连接与长续航。',
  image: '/missing-product.png', providor: '轻行数码', status: 1, agent_comment: '',
  created_at: '2026-09-01T08:00:00Z', updated_at: '2026-09-01T08:00:00Z',
}
const sku: Sku = {
  id: 'sku-1', product_id: product.id, name: '苔绿标准版', specs: '{"颜色":"苔绿","连接":"蓝牙"}',
  price: 19900, stock: 8, sold: 10, status: 1, agent_comment: '', created_at: product.created_at, updated_at: product.updated_at,
}
const order: Order = {
  id: 'order-demo-20260916-000000000001', user_id: user.id, original_amount: 39800, discount_amount: 0, pay_amount: 39800,
  status: 1, pay_type: '', pay_time: '', remark: '请保护好包装', snapshot: '', idempotency_key: 'fixture-key',
  items: [{ product_id: product.id, sku_id: sku.id, sku_name: sku.name, price: sku.price, quantity: 2, discount_amount: 0, total_amount: 39800, snapshot: '' }],
  created_at: product.created_at, updated_at: product.updated_at,
}
const orderUrl = `/orders/${order.id}`
const payment = { out_trade_no: 'PAY-DEMO-20260916', qr_code: 'https://example.invalid/test-payment-only', status: 0 }
const list = <T,>(items: T[], total = items.length, page = 1, pageSize = 12) => ({ list: items, total, page, page_size: pageSize })

test.beforeEach(async ({ page }) => {
  await page.addInitScript((profile) => {
    localStorage.setItem('budgetmatch:token', JSON.stringify('test-token'))
    localStorage.setItem('budgetmatch:userInfo', JSON.stringify(profile))
  }, user)
  await page.route('**/missing-product.png', (route) => route.fulfill({ status: 404, body: '' }))
  // 所有商城请求均隔离为模拟响应，不会创建真实订单或发起真实支付。
  await page.route('http://127.0.0.1:4173/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/mall/products') await route.fulfill({ json: list([product]) })
    else if (path === `/api/mall/products/${product.id}`) await route.fulfill({ json: { product } })
    else if (path === '/api/mall/skus') await route.fulfill({ json: list([{ ...sku, id: 'sku-empty', name: '云白标准版', specs: '{"颜色":"云白","连接":"蓝牙"}', stock: 0 }, sku]) })
    else if (path === '/api/mall/orders' && route.request().method() === 'GET') await route.fulfill({ json: list([order], 1, 1, 10) })
    else if (path === '/api/mall/orders') await route.fulfill({ json: { order_id: order.id, status: 1 } })
    else if (path === `/api/mall/orders/${order.id}`) await route.fulfill({ json: { order } })
    else if (path.endsWith('/pay/query')) await route.fulfill({ json: { status: 0, trade_no: '' } })
    else if (path.endsWith('/pay')) await route.fulfill({ json: payment })
    else if (path === '/api/user/info') await route.fulfill({ json: { user } })
    else await route.fulfill({ status: 404, json: { message: `测试未提供接口：${path}` } })
  })
})

test('商品使用真实字段，缺图回退，进入详情显示规格', async ({ page }) => {
  await page.goto('/products')
  await expect(page.getByText(product.providor, { exact: true })).toBeVisible()
  await expect(page.getByText(product.content, { exact: true })).toBeVisible()
  await expect(page.getByRole('img', { name: `${product.name}暂无图片` })).toBeVisible()
  await page.getByRole('link', { name: /轻行无线耳机/ }).click()
  await expect(page).toHaveURL(`/products/${product.id}`)
  await expect(page.getByRole('radio', { name: /云白标准版/ })).toBeDisabled()
  await expect(page.getByRole('radio', { name: /苔绿标准版/ })).toBeChecked()
  await expect(page.getByText('颜色：苔绿 · 连接：蓝牙', { exact: true })).toBeVisible()
  await expect(page.getByText('商品合计').locator('..')).toContainText('¥199.00')
})

test('搜索和分页写入地址，详情返回恢复筛选', async ({ page }) => {
  const queries: URLSearchParams[] = []
  await page.route('**/api/mall/products?**', (route) => {
    const params = new URL(route.request().url()).searchParams
    queries.push(params)
    return route.fulfill({ json: list([product], 25, Number(params.get('page')), Number(params.get('page_size'))) })
  })
  await page.goto('/products')
  await page.getByRole('textbox', { name: '搜索商品' }).fill('耳机')
  await page.getByRole('button', { name: /^搜\s*索$/ }).click()
  await expect(page).toHaveURL(/keyword=/)
  await expect(page.getByText('“耳机”的搜索结果 · 共 25 件')).toBeVisible()
  await page.getByTitle('2', { exact: true }).click()
  await expect(page).toHaveURL(/page=2/)
  await expect.poll(() => queries.at(-1)?.get('keyword')).toBe('耳机')
  expect(queries.at(-1)?.get('page')).toBe('2')
  expect(queries.at(-1)?.get('status')).toBe('1')
  await page.getByRole('link', { name: /轻行无线耳机/ }).click()
  await page.getByRole('link', { name: '← 返回商品列表' }).click()
  await expect(page).toHaveURL(/keyword=.*page=2/)
  await expect(page.getByRole('textbox', { name: '搜索商品' })).toHaveValue('耳机')
})

test('快速改变搜索时，迟到的旧响应不覆盖新结果', async ({ page }) => {
  let releaseOld: (() => void) | undefined
  let oldResponded = false
  await page.route('**/api/mall/products?**', async (route) => {
    const keyword = new URL(route.request().url()).searchParams.get('keyword')
    if (keyword === '旧商品') await new Promise<void>((resolve) => { releaseOld = resolve })
    await route.fulfill({ json: list([{ ...product, name: keyword || product.name }]) })
    if (keyword === '旧商品') oldResponded = true
  })
  await page.goto('/products')
  await page.getByRole('textbox', { name: '搜索商品' }).fill('旧商品')
  await page.getByRole('button', { name: /^搜\s*索$/ }).click()
  await expect.poll(() => !!releaseOld).toBe(true)
  await page.getByRole('textbox', { name: '搜索商品' }).fill('新商品')
  await page.getByRole('button', { name: /^搜\s*索$/ }).click()
  await expect(page.getByRole('heading', { name: '新商品' })).toBeVisible()
  releaseOld?.()
  await expect.poll(() => oldResponded).toBe(true)
  await expect(page.getByRole('heading', { name: '新商品' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '旧商品' })).toHaveCount(0)
})

test('列表错误可重试，空页保留返回第一页入口', async ({ page }) => {
  let failure = true
  await page.route('**/api/mall/products?**', (route) => route.fulfill(failure
    ? { status: 503, json: { message: '商品服务暂时不可用' } }
    : { json: list([], 12, 2) }))
  await page.goto('/products?page=2')
  await expect(page.getByRole('alert')).toContainText('商品服务暂时不可用')
  failure = false
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.getByText('这一页没有商品了')).toBeVisible()
  await page.getByRole('button', { name: '回到第一页' }).click()
  await expect(page).toHaveURL('/products')
})

test('详情错误可重试，售罄和下架商品不能下单', async ({ page }) => {
  let failure = true
  await page.route(`**/api/mall/products/${product.id}`, (route) => route.fulfill(failure
    ? { status: 503, json: { message: '详情加载暂时失败' } }
    : { json: { product: { ...product, status: 0 } } }))
  await page.goto(`/products/${product.id}`)
  await expect(page.getByRole('alert')).toContainText('详情加载暂时失败')
  failure = false
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.getByText('商品已下架，暂时无法购买')).toBeVisible()
  await expect(page.getByRole('button', { name: '确认购买' })).toBeDisabled()
  await page.route('**/api/mall/skus?**', (route) => route.fulfill({ json: list([{ ...sku, stock: 0 }]) }))
  await page.reload()
  await expect(page.getByText('所有规格暂时缺货，请稍后再来看看')).toBeVisible()
  await expect(page.getByRole('button', { name: '确认购买' })).toHaveCount(0)
})

test('超过单页的规格继续加载，数量上限为 99', async ({ page }) => {
  const requestedPages: string[] = []
  await page.route('**/api/mall/skus?**', (route) => {
    const params = new URL(route.request().url()).searchParams
    expect(params.get('product_id')).toBe(product.id)
    expect(params.get('status')).toBe('1')
    requestedPages.push(params.get('page')!)
    return route.fulfill({ json: list(params.get('page') === '1' ? [{ ...sku, stock: 0 }] : [{ ...sku, id: 'sku-later', stock: 150 }], 2) })
  })
  await page.goto(`/products/${product.id}`)
  await expect(page.getByRole('spinbutton', { name: '购买数量' })).toHaveAttribute('aria-valuemax', '99')
  expect(requestedPages).toContain('2')
  await page.getByRole('spinbutton', { name: '购买数量' }).fill('')
  await expect(page.getByRole('button', { name: '确认购买' })).toBeDisabled()
})

test('下单确认展示准确金额，失败重试复用幂等键并阻止重复点击', async ({ page }) => {
  const requests: Record<string, unknown>[] = []
  let release: (() => void) | undefined
  await page.route('**/api/mall/orders', async (route) => {
    requests.push(route.request().postDataJSON())
    if (requests.length === 1) {
      await new Promise<void>((resolve) => { release = resolve })
      await route.fulfill({ status: 503, json: { message: '提交结果暂未确认' } })
    } else await route.fulfill({ json: { order_id: order.id, status: 1 } })
  })
  await page.goto(`/products/${product.id}`)
  await page.getByRole('spinbutton', { name: '购买数量' }).fill('2')
  await page.getByRole('textbox', { name: '订单备注（选填）' }).fill('请保护好包装')
  await page.getByRole('button', { name: '确认购买' }).click()
  const dialog = page.getByRole('dialog', { name: '确认订单' })
  await expect(dialog).toContainText('¥398.00')
  expect(requests).toHaveLength(0)
  await dialog.getByRole('button', { name: '提交订单' }).dblclick()
  await expect.poll(() => requests.length).toBe(1)
  await expect(dialog.getByRole('button', { name: '再想想' })).toBeDisabled()
  release?.()
  await expect(dialog.getByRole('alert')).toContainText('提交结果暂未确认')
  await dialog.getByRole('button', { name: '重试提交' }).click()
  await expect(page).toHaveURL(orderUrl)
  expect(requests).toHaveLength(2)
  expect(requests[0]).toEqual({ sku_id: sku.id, quantity: 2, remark: order.remark, idempotency_key: expect.any(String) })
  expect(requests[0].idempotency_key).toBe(requests[1].idempotency_key)
})

test('订单筛选正确传参，详情保留列表筛选', async ({ page }) => {
  const statuses: string[] = []
  await page.route('**/api/mall/orders?**', (route) => {
    const status = new URL(route.request().url()).searchParams.get('status')!
    statuses.push(status)
    return route.fulfill({ json: list([{ ...order, status: status === '-1' ? 1 : Number(status) }]) })
  })
  await page.goto('/orders')
  await expect(page.getByText(order.id, { exact: true })).toBeVisible()
  expect(statuses).toContain('-1')
  await page.getByRole('tab', { name: '已支付', exact: true }).click()
  await expect(page).toHaveURL('/orders?status=2')
  await page.getByRole('link', { name: '查看详情 →' }).click()
  await page.getByRole('link', { name: '← 返回订单列表' }).click()
  await expect(page).toHaveURL('/orders?status=2')
  expect(statuses.at(-1)).toBe('2')
})

test('取消订单需要确认，服务失败不伪装为取消成功', async ({ page }) => {
  let cancelled = false
  let calls = 0
  await page.route(`**/api/mall/orders/${order.id}`, (route) => route.fulfill({ json: { order: { ...order, status: cancelled ? 5 : 1 } } }))
  await page.route(`**/api/mall/orders/${order.id}/cancel`, (route) => {
    calls++
    if (calls === 1) return route.fulfill({ status: 503, json: { message: '取消暂时失败' } })
    cancelled = true
    return route.fulfill({ status: 200, body: '' })
  })
  await page.goto(orderUrl)
  await page.getByRole('button', { name: '取消订单' }).click()
  expect(calls).toBe(0)
  const dialog = page.getByRole('dialog', { name: '确认取消订单？' })
  await dialog.getByRole('button', { name: '确认取消' }).click()
  await expect(dialog.getByRole('alert')).toContainText('取消暂时失败')
  await dialog.getByRole('button', { name: '确认取消' }).click()
  await expect(page.getByText('已取消', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '去支付' })).toHaveCount(0)
})

async function openPayment(page: Page) {
  await page.goto(orderUrl)
  await page.getByRole('button', { name: '去支付' }).click()
  return page.getByRole('dialog', { name: '支付宝扫码支付' })
}

test('支付流水 0 是待支付，1 才是成功，并刷新订单', async ({ page }) => {
  let paid = false
  await page.route('**/pay/query', (route) => route.fulfill({ json: { status: paid ? 1 : 0, trade_no: paid ? 'trade-test' : '' } }))
  await page.route(`**/api/mall/orders/${order.id}`, (route) => route.fulfill({ json: { order: { ...order, status: paid ? 2 : 1, pay_type: paid ? 'alipay' : '' } } }))
  const dialog = await openPayment(page)
  await expect(dialog.getByText('正在等待支付结果…')).toBeVisible()
  await expect(dialog.getByText('支付成功', { exact: true })).toHaveCount(0)
  paid = true
  await dialog.getByRole('button', { name: '我已支付，查询结果' }).click()
  await expect(dialog.getByText('支付成功', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: /^完\s*成$/ }).click()
  await expect(page.getByText('已支付', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '去支付' })).toHaveCount(0)
})

test('支付流水 2 表示关闭，不会误报成功', async ({ page }) => {
  await page.route('**/pay/query', (route) => route.fulfill({ json: { status: 2, trade_no: '' } }))
  const dialog = await openPayment(page)
  await expect(dialog.getByText('支付已关闭', { exact: true })).toBeVisible()
  await expect(dialog.getByText('支付成功', { exact: true })).toHaveCount(0)
  await expect(dialog.locator('canvas')).toHaveCount(0)
})

test('创建支付直接返回成功时不展示空二维码', async ({ page }) => {
  let creates = 0
  await page.route('**/api/mall/orders/*/pay', (route) => {
    creates++
    return route.fulfill({ json: { ...payment, qr_code: '', status: 1 } })
  })
  const dialog = await openPayment(page)
  await expect(dialog.getByText('支付成功', { exact: true })).toBeVisible()
  expect(creates).toBe(1)
  await expect(dialog.locator('canvas')).toHaveCount(0)
  await dialog.getByRole('button', { name: /^完\s*成$/ }).click()
  await expect(page.getByText('已确认支付成功，订单状态同步中')).toBeVisible()
  await expect(page.getByRole('button', { name: '去支付' })).toBeDisabled()
})

test('支付查询失败可手动重试，关闭弹窗停止后续轮询', async ({ page }) => {
  await page.clock.install()
  let calls = 0
  let failure = true
  await page.route('**/pay/query', (route) => {
    calls++
    return route.fulfill(failure ? { status: 503, json: { message: '查询暂不可用' } } : { json: { status: 0, trade_no: '' } })
  })
  const dialog = await openPayment(page)
  await expect(dialog.getByRole('alert')).toContainText('支付状态查询失败')
  failure = false
  await dialog.getByRole('button', { name: '我已支付，查询结果' }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(dialog.getByRole('alert')).toHaveCount(0)
  await dialog.getByRole('button', { name: /^关\s*闭$/ }).click()
  await expect(dialog).toHaveCount(0)
  const stoppedAt = calls
  await page.clock.runFor(10000)
  expect(calls).toBe(stoppedAt)
})

test('订单加载错误可重试，空明细不导致页面崩溃', async ({ page }) => {
  let failure = true
  await page.route(`**/api/mall/orders/${order.id}`, (route) => route.fulfill(failure
    ? { status: 503, json: { message: '订单服务暂不可用' } }
    : { json: { order: { ...order, items: null } } }))
  await page.goto(orderUrl)
  await expect(page.getByRole('alert')).toContainText('订单服务暂不可用')
  failure = false
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await expect(page.getByText('暂无商品明细')).toBeVisible()
})

test('订单列表加载失败可重试，取消后刷新当前筛选', async ({ page }) => {
  let failure = true
  let cancelled = false
  await page.route('**/api/mall/orders?**', (route) => route.fulfill(failure
    ? { status: 503, json: { message: '订单列表暂不可用' } }
    : { json: list(cancelled ? [] : [order]) }))
  await page.route(`**/api/mall/orders/${order.id}/cancel`, (route) => {
    cancelled = true
    return route.fulfill({ status: 200, body: '' })
  })
  await page.goto('/orders?status=1')
  await expect(page.getByRole('alert')).toContainText('订单列表暂不可用')
  failure = false
  await page.getByRole('button', { name: /^重\s*试$/ }).click()
  await page.getByRole('button', { name: '取消订单' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '保留订单' }).click()
  expect(cancelled).toBe(false)
  await page.getByRole('button', { name: '取消订单' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认取消' }).click()
  await expect(page.getByText('暂时没有待支付的订单')).toBeVisible()
  await expect(page).toHaveURL('/orders?status=1')
})

test('创建支付失败可重试，未知状态不展示可付款二维码', async ({ page }) => {
  let calls = 0
  await page.route('**/api/mall/orders/*/pay', (route) => {
    calls++
    return route.fulfill(calls === 1 ? { status: 503, json: { message: '支付服务暂不可用' } } : { json: { ...payment, status: 99 } })
  })
  await page.goto(orderUrl)
  await page.getByRole('button', { name: '去支付' }).click()
  await expect(page.getByRole('alert')).toContainText('支付服务暂不可用')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('button', { name: '去支付' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('alert')).toContainText('无法识别支付状态')
  await expect(dialog.locator('canvas')).toHaveCount(0)
})

test('关闭支付弹窗后，迟到的查询响应不再更新页面', async ({ page }) => {
  let release: (() => void) | undefined
  let responded = false
  let detailRequests = 0
  await page.route(`**/api/mall/orders/${order.id}`, (route) => {
    detailRequests++
    return route.fulfill({ json: { order } })
  })
  await page.route('**/pay/query', async (route) => {
    await new Promise<void>((resolve) => { release = resolve })
    await route.fulfill({ json: { status: 1, trade_no: 'late-trade' } })
    responded = true
  })
  const dialog = await openPayment(page)
  await expect.poll(() => !!release).toBe(true)
  await dialog.getByRole('button', { name: /^关\s*闭$/ }).click()
  const detailsBefore = detailRequests
  release?.()
  await expect.poll(() => responded).toBe(true)
  await expect(dialog).toHaveCount(0)
  await expect(page.getByText('支付成功', { exact: true })).toHaveCount(0)
  expect(detailRequests).toBe(detailsBefore)
})

for (const width of [360, 768, 1280]) {
  test(`商城四个页面在 ${width}px 下布局正常`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    for (const [name, path, ready] of [
      ['products', '/products', product.name],
      ['product-detail', `/products/${product.id}`, '关于这件商品'],
      ['orders', '/orders', order.id],
      ['order-detail', orderUrl, '商品清单'],
    ]) {
      await page.goto(path)
      await expect(page.getByText(ready, { exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
      await page.screenshot({ path: testInfo.outputPath(`${name}.png`), fullPage: true })
    }
  })
}
