/** 只接受站内目标，并避免登录、注册页面之间循环跳转。 */
export function getReturnTo(search: string): string {
  const value = new URLSearchParams(search).get('redirect')
  if (!value || !value.startsWith('/') || value.startsWith('//') || /[\\\s]/.test(value)) return '/'

  try {
    const url = new URL(value, 'https://budgetmatch.local')
    const pathname = decodeURIComponent(url.pathname)
    if (url.origin !== 'https://budgetmatch.local' || pathname.startsWith('//') || pathname.includes('\\') || /^\/(login|register)(\/|$)/.test(pathname)) return '/'
    return `${url.pathname}${url.search}${url.hash}`
  } catch {
    return '/'
  }
}

export function authPath(page: 'login' | 'register', returnTo = '/') {
  return returnTo === '/' ? `/${page}` : `/${page}?${new URLSearchParams({ redirect: returnTo })}`
}
