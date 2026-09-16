import { useCallback, useEffect, useState } from 'react'

// load 须用 useCallback 固定引用；切换查询时立即隐藏旧数据，并中止旧请求。
export function useResource<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [revision, setRevision] = useState(0)
  const [result, setResult] = useState<{
    load: typeof load; revision: number; data?: T; error?: Error
  }>()

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal).then(
      (data) => { if (!controller.signal.aborted) setResult({ load, revision, data }) },
      (error: Error) => { if (!controller.signal.aborted) setResult({ load, revision, error }) },
    )
    return () => controller.abort()
  }, [load, revision])

  const current = result?.load === load && result.revision === revision ? result : undefined
  const reload = useCallback(() => setRevision((value) => value + 1), [])
  return { data: current?.data, error: current?.error, loading: !current, reload }
}
