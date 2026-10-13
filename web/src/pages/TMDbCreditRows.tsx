import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { tmdbCenterAPI, type TMDbCredit } from '../api/tmdbCenter'
import type { MediaCredit } from '../types'
import { MediaCredits } from './MediaDetailCast'
import { tmdbJob } from './tmdbLabels'

export function TMDbCreditRows({ id, onPersonClick }: { id: string; onPersonClick: (credit: MediaCredit) => void }) {
  return <div className="min-w-0 space-y-6"><CreditRow id={id} category="cast" onPersonClick={onPersonClick} /><CreditRow id={id} category="crew" onPersonClick={onPersonClick} /></div>
}

function CreditRow({ id, category, onPersonClick }: { id: string; category: 'cast' | 'crew'; onPersonClick: (credit: MediaCredit) => void }) {
  const [rows, setRows] = useState<TMDbCredit[]>([])
  const [total, setTotal] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [settled, setSettled] = useState(0)
  const page = useRef(0)
  const request = useRef<AbortController | null>(null)
  const load = useCallback(() => {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setLoading(true); setError('')
    const next = page.current + 1
    void tmdbCenterAPI.credits(id, false, next, controller.signal, { category, page_size: 20 }).then(result => {
      if (controller.signal.aborted) return
      // 只提交成功页，失败重试仍请求同一页；追加保留已有卡片和滚动位置。
      page.current = next
      setRows(previous => {
        const seen = new Set(previous.map(v => v.id))
        return [...previous, ...result.data.filter(v => !seen.has(v.id))]
      })
      setTotal(result.total)
      setHasMore(next * 20 < result.total)
    }).catch(() => { if (!controller.signal.aborted) setError('读取失败，已有内容已保留') })
      .finally(() => { if (!controller.signal.aborted) { setLoading(false); setSettled(value => value + 1) } })
  }, [id, category])
  // 等失败/成功状态提交到界面后才解锁，避免同一轮滚动事件抢先重复请求。
  useLayoutEffect(() => { request.current = null }, [settled])
  useEffect(() => {
    load()
    return () => { request.current?.abort(); request.current = null }
  }, [load])
  return <MediaCredits progressive title={category === 'cast' ? '演员' : '幕后'} total={total} loading={loading} error={error}
    credits={rows.map(v => ({ person_id: v.person.person_id, name: v.person.name, profile_url: v.person.profile_url, type: v.category, role: [v.character, tmdbJob(v.job)].filter(Boolean).join(' / ') }))}
    onPersonClick={onPersonClick} onLoadMore={error || hasMore ? load : undefined} />
}
