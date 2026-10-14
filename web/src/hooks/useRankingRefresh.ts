import { useEffect, useRef, useState } from 'react'

// 榜单切换会卸载调用页面；取消旧刷新，避免迟到响应重置新榜单。
export function useRankingRefresh(request: (signal: AbortSignal) => Promise<{ updated_at: string | null }>, onSuccess: (updatedAt: string | null) => void) {
  const active = useRef<AbortController | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshError, setRefreshError] = useState('')
  useEffect(() => () => { active.current?.abort() }, [])
  const refreshRank = async () => {
    if (active.current) return
    const controller = new AbortController()
    active.current = controller
    setRefreshing(true)
    setRefreshError('')
    try {
      const result = await request(controller.signal)
      if (!controller.signal.aborted) onSuccess(result.updated_at)
    } catch {
      if (!controller.signal.aborted) setRefreshError('榜单刷新未完成，已保留原榜单和更新时间，请稍后重试。')
    } finally {
      if (!controller.signal.aborted) { active.current = null; setRefreshing(false) }
    }
  }
  return { refreshing, refreshError, refreshRank }
}
