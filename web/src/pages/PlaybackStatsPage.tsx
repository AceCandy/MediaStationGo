import { useCallback, useEffect, useMemo, useState } from 'react'
import { BarChart3, RefreshCw } from 'lucide-react'

import { adminAPI, type PlaybackStatsQuery, type PlaybackStatsResult } from '../api/admin'
import { libraryAPI } from '../api/library'
import type { Library, User } from '../types'

function localDate(date: Date): string {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 10)
}

function defaultDates() {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 29)
  return { from: localDate(from), to: localDate(to) }
}

export function PlaybackStatsPage() {
  const dates = useMemo(() => defaultDates(), [])
  const [grain, setGrain] = useState<PlaybackStatsQuery['grain']>('day')
  const [from, setFrom] = useState(dates.from)
  const [to, setTo] = useState(dates.to)
  const [userID, setUserID] = useState('')
  const [mediaType, setMediaType] = useState<'' | 'movie' | 'tv'>('')
  const [libraryIDs, setLibraryIDs] = useState<string[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [data, setData] = useState<PlaybackStatsResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    void Promise.all([adminAPI.listUsers(), libraryAPI.list({ includeHidden: true })])
      .then(([userRows, libraryRows]) => {
        setUsers(userRows)
        setLibraries(libraryRows)
      })
      .catch(() => setError('筛选项加载失败。'))
  }, [])

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    return adminAPI.playbackStats({
      grain,
      from,
      to,
      user_id: userID || undefined,
      media_type: mediaType || undefined,
      library_ids: libraryIDs.length > 0 ? libraryIDs.join(',') : undefined,
    }).then(setData).catch(() => setError('播放统计加载失败。')).finally(() => setLoading(false))
  }, [from, grain, libraryIDs, mediaType, to, userID])

  useEffect(() => { void load() }, [load])

  const maxCount = Math.max(1, ...(data?.buckets.map((bucket) => bucket.count) ?? []))

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <BarChart3 className="h-7 w-7 text-brand-500" />
        <div><h1 className="page-heading">播放统计</h1><p className="page-subtitle">按真实播放会话查看日、周和月播放次数。</p></div>
      </header>

      <section className="glass-panel space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <label className="text-xs text-ink-50">粒度<select className="input-base mt-1" value={grain} onChange={(event) => setGrain(event.target.value as PlaybackStatsQuery['grain'])}><option value="day">每日</option><option value="week">每周</option><option value="month">每月</option></select></label>
          <label className="text-xs text-ink-50">开始日期<input className="input-base mt-1" type="date" value={from} max={to} onChange={(event) => setFrom(event.target.value)} /></label>
          <label className="text-xs text-ink-50">结束日期<input className="input-base mt-1" type="date" value={to} min={from} onChange={(event) => setTo(event.target.value)} /></label>
          <label className="text-xs text-ink-50">账户<select className="input-base mt-1" value={userID} onChange={(event) => setUserID(event.target.value)}><option value="">全部账户</option>{users.map((user) => <option key={user.id} value={user.id}>{user.nickname || user.username}</option>)}</select></label>
          <label className="text-xs text-ink-50">媒体类型<select className="input-base mt-1" value={mediaType} onChange={(event) => setMediaType(event.target.value as '' | 'movie' | 'tv')}><option value="">全部类型</option><option value="movie">电影</option><option value="tv">电视剧</option></select></label>
        </div>
        <fieldset>
          <legend className="text-xs text-ink-50">媒体库（不选表示全部）</legend>
          <div className="mt-2 flex max-h-32 flex-wrap gap-2 overflow-auto">
            {libraries.map((library) => <label key={library.id} className="badge-neutral cursor-pointer"><input type="checkbox" className="mr-2 accent-brand-500" checked={libraryIDs.includes(library.id)} onChange={(event) => setLibraryIDs((current) => event.target.checked ? [...current, library.id] : current.filter((id) => id !== library.id))} />{library.name}</label>)}
          </div>
        </fieldset>
        <button type="button" className="btn-outline inline-flex items-center gap-2" onClick={() => void load()}><RefreshCw size={16} />刷新</button>
      </section>

      <section className="glass-panel">
        <p className="text-sm text-ink-50">播放总次数</p>
        <p className="mt-1 text-4xl font-bold text-ink-600">{data?.total ?? 0}</p>
      </section>

      <section className="glass-panel space-y-4" aria-label="播放次数时间序列">
        {loading && !data ? <p className="py-8 text-center text-ink-50">加载中...</p> : error ? <p className="py-8 text-center text-red-500">{error}</p> : !data?.buckets.length ? <p className="py-8 text-center text-ink-50">当前条件下暂无播放记录。</p> : data.buckets.map((bucket) => (
          <div key={bucket.period} className="grid grid-cols-[6rem_minmax(0,1fr)_3rem] items-center gap-3 text-sm">
            <span className="text-ink-50">{bucket.period}</span>
            <div className="h-3 overflow-hidden rounded-full bg-gray-200" role="img" aria-label={`${bucket.period} 播放 ${bucket.count} 次`}><div className="h-full rounded-full bg-brand-500" style={{ width: `${Math.max(2, bucket.count / maxCount * 100)}%` }} /></div>
            <span className="text-right font-semibold text-ink-600">{bucket.count}</span>
          </div>
        ))}
      </section>
    </div>
  )
}
