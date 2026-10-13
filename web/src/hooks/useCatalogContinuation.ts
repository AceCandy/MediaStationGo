import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

interface Page<T> { items: T[]; total: number; page_size: number }

// 页码只在成功后推进；失败保留已展示资料，筛选变化取消旧请求。
export function useCatalogContinuation<T extends { id: string }>(fetchPage: (page: number, signal: AbortSignal) => Promise<Page<T>>, revision: number, paused: boolean) {
  const [items, setItems] = useState<T[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [hasMore, setHasMore] = useState(false)
  const [settled, setSettled] = useState(0)
  const cursor = useRef(0)
  const active = useRef<AbortController | null>(null)
  const sentinel = useRef<HTMLDivElement>(null)

  const request = useCallback((page: number) => {
    if (active.current) return
    const c = new AbortController()
    active.current = c
    setLoading(true)
    setError('')
    void fetchPage(page, c.signal).then(result => {
      if (c.signal.aborted) return
      setItems(previous => Array.from(new Map([...(page === 1 ? [] : previous), ...result.items].map(item => [item.id, item])).values()))
      setTotal(result.total)
      setHasMore(result.items.length > 0 && page * result.page_size < result.total)
      cursor.current = page
    }).catch((e: unknown) => {
      if (!c.signal.aborted) setError((e as { response?: { data?: { message?: string } } })?.response?.data?.message || '资料列表读取失败，请重试')
    }).finally(() => {
      if (c.signal.aborted) return
      setLoading(false)
      setSettled(value => value + 1)
    })
  }, [fetchPage])

  // 等响应状态提交后解锁，避免滚动事件在错误尚未呈现时自动重发。
  useLayoutEffect(() => { active.current = null }, [settled])
  useEffect(() => {
    cursor.current = 0
    setItems([]); setTotal(0); setHasMore(false)
    request(1)
    return () => { active.current?.abort(); active.current = null }
  }, [request, revision])
  const loadMore = useCallback(() => { if (!loading && (hasMore || error || cursor.current === 0)) request(cursor.current + 1) }, [loading, hasMore, error, request])
  useEffect(() => {
    const node = sentinel.current
    if (!node || paused || loading || error || !hasMore) return
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) loadMore() }, { rootMargin: '320px' })
    observer.observe(node)
    return () => observer.disconnect()
  }, [paused, loading, error, hasMore, loadMore])
  return { items, total, loading, error, hasMore, loadMore, sentinel }
}
