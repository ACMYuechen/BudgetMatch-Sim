import { expect, test, type Page } from '@playwright/test'
import { authPath, getReturnTo } from '../src/utils/authNavigation'

const user = {
  id: 'user-demo', username: '小林', email: 'lin@example.com',
  avatar: '', phone: '', role: 100, status: 1, remark: '',
}

async function signIn(page: Page) {
  await page.getByLabel('用户名', { exact: true }).fill('xiaolin')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByRole('button', { name: /^登\s*录$/ }).click()
}

async function storedSession(page: Page) {
  await page.addInitScript((profile) => {
    localStorage.setItem('budgetmatch:token', JSON.stringify('test-token'))
    localStorage.setItem('budgetmatch:userInfo', JSON.stringify(profile))
  }, user)
}

// 浏览器测试使用真实前端和契约格式的模拟响应，不连接数据库、发邮件或调用模型。
test.beforeEach(async ({ page }) => {
  await page.route('http://127.0.0.1:4173/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/auth/login/username' || path === '/api/auth/login/email') {
      await route.fulfill({ json: { token: 'test-token', user_id: user.id, role: user.role } })
    } else if (path === '/api/user/info') {
      expect(route.request().headers().authorization).toBe('Bearer test-token')
      await route.fulfill({ json: { user } })
    } else if (path === '/api/agent/conversations') {
      await route.fulfill({ json: { list: [], total: 0, page: 1, page_size: 100 } })
    } else if (path === '/api/auth/code/send' || path === '/api/auth/register') {
      await route.fulfill({ json: { success: true } })
    } else if (path === '/api/mall/products' || path === '/api/mall/orders') {
      await route.fulfill({ json: { list: [], total: 0, page: 1, page_size: 12 } })
    } else {
      await route.fulfill({ status: 404, json: { message: `测试未提供此接口：${path}` } })
    }
  })
})

test('首页需求在登录后保留到推荐表单', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: '开始我的预算计划' }).click()
  await expect(page.getByText('先说说你想买什么吧')).toBeVisible()
  await page.getByRole('button', { name: '轻装通勤' }).click()
  await page.getByRole('button', { name: '开始我的预算计划' }).click()
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await signIn(page)
  await expect(page).toHaveURL(/\/recommend\?query=/)
  await expect(page.getByRole('textbox')).toHaveValue('想选一副通勤耳机，优先便携和续航')
  await expect(page.getByLabel('本轮预算（元）')).toHaveValue('800.00')
  await expect(page.getByRole('button', { name: '账户菜单' })).toContainText(user.username)
})

test('登录错误保留表单和目标页面', async ({ page }) => {
  await page.route('**/api/auth/login/username', (route) => route.fulfill({
    status: 401, json: { code: 401001, message: '用户名或密码不正确' },
  }))
  await page.goto('/orders?status=1#recent')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await signIn(page)
  await expect(page.getByRole('alert')).toContainText('用户名或密码不正确')
  await expect(page.getByLabel('用户名', { exact: true })).toHaveValue('xiaolin')
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/orders?status=1#recent')
})

test('商品页面在发起接口请求之前要求登录', async ({ page }) => {
  let productRequests = 0
  page.on('request', (request) => { if (request.url().includes('/api/mall/')) productRequests++ })
  await page.goto('/products/product-42?sku=42')
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/products/product-42?sku=42')
  expect(productRequests).toBe(0)
})

test('邮箱登录使用邮箱接口并返回原页面', async ({ page }) => {
  await page.goto('/login?redirect=%2Fprofile')
  await page.getByRole('tab', { name: '邮箱登录' }).click()
  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  const loginRequest = page.waitForRequest('**/api/auth/login/email')
  await page.getByRole('button', { name: /^登\s*录$/ }).click()
  expect((await loginRequest).postDataJSON()).toEqual({ email: user.email, password: 'test-password' })
  await expect(page).toHaveURL('/profile')
  await expect(page.getByText(user.email, { exact: true })).toBeVisible()
})

test('注册校验、验证码倒计时与登录回跳', async ({ page }) => {
  let codeRequests = 0
  page.on('request', (request) => { if (request.url().endsWith('/api/auth/code/send')) codeRequests++ })
  await page.goto('/register?redirect=%2Fprofile')
  await page.getByLabel('邮箱', { exact: true }).fill('invalid-email')
  await page.getByRole('button', { name: '获取验证码' }).click()
  await expect(page.getByText('请输入有效的邮箱地址')).toBeVisible()
  expect(codeRequests).toBe(0)

  await page.getByLabel('邮箱', { exact: true }).fill(user.email)
  await page.getByRole('button', { name: '获取验证码' }).click()
  await expect(page.getByRole('button', { name: /s 后重试/ })).toBeDisabled()
  expect(codeRequests).toBe(1)
  await page.getByLabel('用户名', { exact: true }).fill('xiaolin')
  await page.getByRole('textbox', { name: '邮箱验证码', exact: true }).fill('123456')
  await page.getByLabel('密码', { exact: true }).fill('123')
  await page.getByLabel('确认密码', { exact: true }).fill('456')
  await page.getByRole('button', { name: '创建账户' }).click()
  await expect(page.getByText('密码至少 6 个字符')).toBeVisible()
  await expect(page.getByText('两次输入的密码不一致')).toBeVisible()

  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByLabel('确认密码', { exact: true }).fill('test-password')
  const registration = page.waitForRequest('**/api/auth/register')
  await page.getByRole('button', { name: '创建账户' }).click()
  expect((await registration).postDataJSON()).toEqual({
    username: 'xiaolin', email: user.email, password: 'test-password', code: '123456',
  })
  await expect(page).toHaveURL('/login?redirect=%2Fprofile')
  await signIn(page)
  await expect(page).toHaveURL('/profile')
})

test('个人中心读取包装后的用户对象，失败可重试', async ({ page }) => {
  await storedSession(page)
  const requests: string[] = []
  page.on('request', (request) => { if (request.url().includes('/api/user/')) requests.push(new URL(request.url()).pathname) })
  await page.goto('/profile')
  await expect(page.getByText(user.email, { exact: true })).toBeVisible()
  await expect(page.getByText(user.id, { exact: true })).toBeVisible()

  let unavailable = true
  await page.route('**/api/user/info', (route) => unavailable
    ? route.fulfill({ status: 503, json: { message: '服务暂时不可用' } })
    : route.fulfill({ json: { user } }))
  await page.getByRole('button', { name: '刷新信息' }).click()
  await expect(page.getByRole('alert')).toContainText('账户信息加载失败')
  await expect(page).toHaveURL('/profile')
  unavailable = false
  await page.getByRole('button', { name: /重\s*试/ }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  expect(requests.every((path) => path === '/api/user/info')).toBe(true)
})

test('过期登录退出到带回跳地址的登录页', async ({ page }) => {
  await storedSession(page)
  await page.route('**/api/user/info', (route) => route.fulfill({ status: 401, body: 'invalid token' }))
  await page.goto('/profile')
  await expect(page).toHaveURL('/login?redirect=%2Fprofile')
  expect(await page.evaluate(() => localStorage.getItem('budgetmatch:token'))).toBeNull()
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
})

test('推荐 SSE 的 401 与普通请求采用相同的退出逻辑', async ({ page }) => {
  await storedSession(page)
  await page.route('**/api/agent/recommend/stream', (route) => route.fulfill({ status: 401, body: 'invalid token' }))
  await page.goto('/recommend')
  await page.getByRole('textbox').fill('买一副通勤耳机')
  await page.getByRole('button', { name: '发送需求' }).click()
  await expect(page).toHaveURL('/login?redirect=%2Frecommend')
  expect(await page.evaluate(() => localStorage.getItem('budgetmatch:token'))).toBeNull()
})

test('移动端导航可以打开、跳转，退出后清除会话', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await storedSession(page)
  await page.goto('/')
  await page.getByRole('button', { name: '打开导航菜单' }).click()
  const navigation = page.getByRole('navigation', { name: '移动端导航' })
  await navigation.getByRole('link', { name: '预算推荐' }).click()
  await expect(page).toHaveURL('/recommend')
  await expect(navigation).not.toBeVisible()
  await page.getByRole('button', { name: '账户菜单' }).click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await expect(page).toHaveURL('/login?redirect=%2Frecommend')
})

test('未知页面提供返回首页入口', async ({ page }) => {
  await page.goto('/does-not-exist')
  await expect(page.getByText('这个页面不见了')).toBeVisible()
  await page.getByRole('link', { name: '返回首页' }).click()
  await expect(page.getByRole('heading', { name: '这次，想买点什么？' })).toBeVisible()
})

test('回跳目标只接受站内地址，保留查询和片段', () => {
  for (const target of ['https://example.com', '//example.com', '/\\example.com', '/login', '/register?redirect=/login', '/foo/../login', '/log%69n', '/%2fexample.com']) {
    expect(getReturnTo(`?${new URLSearchParams({ redirect: target })}`)).toBe('/')
  }
  const target = '/orders/order-1?tab=payment#details'
  expect(getReturnTo(authPath('login', target).split('?')[1])).toBe(target)
})

for (const width of [360, 768, 1280]) {
  test(`首页和注册页在 ${width}px 下没有横向溢出`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    for (const path of ['/', '/register']) {
      await page.goto(path)
      await expect(page.getByRole('button', { name: path === '/' ? '开始我的预算计划' : '创建账户' })).toBeVisible()
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)
      expect(overflow).toBe(false)
      await page.screenshot({ path: testInfo.outputPath(path === '/' ? 'home.png' : 'register.png'), fullPage: true })
    }
  })
}
