const PREFIX = 'budgetmatch:'

export function setItem<T>(key: string, value: T): void {
  try {
    localStorage.setItem(`${PREFIX}${key}`, JSON.stringify(value))
  } catch {
    // ignore
  }
}

export function getItem<T>(key: string, defaultValue?: T): T | undefined {
  try {
    const raw = localStorage.getItem(`${PREFIX}${key}`)
    if (raw === null) return defaultValue
    return JSON.parse(raw) as T
  } catch {
    return defaultValue
  }
}

export function removeItem(key: string): void {
  localStorage.removeItem(`${PREFIX}${key}`)
}

export function clear(): void {
  const keys: string[] = []
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i)
    if (k?.startsWith(PREFIX)) keys.push(k)
  }
  keys.forEach((k) => localStorage.removeItem(k))
}
