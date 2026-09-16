export function readPage(value: string | null): number {
  const page = Number(value)
  return Number.isInteger(page) && page > 0 && page <= 100000 ? page : 1
}

export function readPageSize(value: string | null, options: number[]): number {
  const size = Number(value)
  return options.includes(size) ? size : options[0]
}

export function formatSpecs(specs: string): string {
  if (!specs) return '标准规格'
  try {
    const parsed: unknown = JSON.parse(specs)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const entries = Object.entries(parsed).filter(([, value]) => ['string', 'number', 'boolean'].includes(typeof value))
      if (entries.length) return entries.map(([key, value]) => `${key}：${value}`).join(' · ')
    }
  } catch { /* 非 JSON 规格以纯文本显示。 */ }
  return specs
}
