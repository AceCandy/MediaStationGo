import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { BarChart3, ChevronLeft, ChevronRight, Film, RefreshCw, Trophy } from 'lucide-react'
import { Link } from 'react-router-dom'

import {
  adminAPI,
  type PlaybackStatsDetail,
  type PlaybackStatsQuery,
  type PlaybackStatsRankItem,
  type PlaybackStatsResult,
} from '../api/admin'
import { imageURL } from '../api/client'
import { libraryAPI } from '../api/library'
import type { Library, User } from '../types'

const DETAIL_PAGE_SIZE = 20

function localDate(date: Date): string {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 10)
}

function defaultDates() {
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - 29)
  return { from: localDate(from), to: localDate(to) }
}

function detailTitle(item: PlaybackStatsDetail): string {
  const episode = item.season_num || item.episode_num
    ? `S${String(item.season_num ?? 0).padStart(2, '0')}E${String(item.episode_num ?? 0).padStart(2, '0')}`
    : ''
  return [item.series_title, episode, item.title !== item.series_title ? item.title : ''].filter(Boolean).join(' · ') || '媒体已不可用'
}

function rankTitle(item: PlaybackStatsRankItem): string {
  return `${item.series_title || item.title}${item.season_num !== undefined ? ` · 第 ${item.season_num} 季` : ''}`
}

function StatsPoster({ src, alt, className }: { src?: string; alt: string; className: string }) {
  return src ? (
    <img src={imageURL(src)} alt={alt} className={`${className} object-cover`} loading="lazy" />
  ) : (
    <div className={`${className} flex items-center justify-center bg-gray-200 text-ink-50`} aria-hidden="true">
      <Film size={20} />
    </div>
  )
}

export function PlaybackStatsPage() {
  const dates = useMemo(() => defaultDates(), [])
  const [grain, setGrain] = useState<PlaybackStatsQuery['grain']>('day')
  const [from, setFrom] = useState(dates.from)
  const [to, setTo] = useState(dates.to)
  const [userID, setUserID] = useState('')
  const [mediaType, setMediaType] = useState<'' | 'movie' | 'tv'>('')
  const [libraryIDs, setLibraryIDs] = useState<string[]>([])
  const [rankGrain, setRankGrain] = useState<'day' | 'week'>('day')
  const [rankDate, setRankDate] = useState(dates.to)
  const [page, setPage] = useState(1)
  const [users, setUsers] = useState<User[]>([])
  const [libraries, setLibraries] = useState<Library[]>([])
  const [data, setData] = useState<PlaybackStatsResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filterError, setFilterError] = useState('')
  const requestID = useRef(0)

  useEffect(() => {
    void Promise.all([adminAPI.listUsers(), libraryAPI.list({ includeHidden: true })])
      .then(([userRows, libraryRows]) => {
        setUsers(userRows)
        setLibraries(libraryRows)
      })
      .catch(() => setFilterError('筛选项加载失败。'))
  }, [])

  const load = useCallback(() => {
    const currentRequest = ++requestID.current
    setLoading(true)
    setError('')
    return adminAPI.playbackStats({
      grain,
      from,
      to,
      user_id: userID || undefined,
      media_type: mediaType || undefined,
      library_ids: libraryIDs.length > 0 ? libraryIDs.join(',') : undefined,
      page,
      page_size: DETAIL_PAGE_SIZE,
      rank_grain: rankGrain,
      rank_date: rankDate,
    }).then((result) => {
      if (requestID.current === currentRequest) setData(result)
    }).catch(() => {
      if (requestID.current === currentRequest) setError('播放统计加载失败。')
    }).finally(() => {
      if (requestID.current === currentRequest) setLoading(false)
    })
  }, [from, grain, libraryIDs, mediaType, page, rankDate, rankGrain, to, userID])

  useEffect(() => { void load() }, [load])

  const maxCount = Math.max(1, ...(data?.buckets.map((bucket) => bucket.count) ?? []))
  const rankMax = Math.max(1, ...(data?.ranking.items.map((item) => item.count) ?? []))
  const pages = Math.max(1, Math.ceil((data?.details.total ?? 0) / DETAIL_PAGE_SIZE))

  const changeFrom = (value: string) => {
    setFrom(value)
    if (rankDate < value) setRankDate(value)
    setPage(1)
  }
  const changeTo = (value: string) => {
    setTo(value)
    if (rankDate > value) setRankDate(value)
    setPage(1)
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <BarChart3 className="h-7 w-7 text-brand-500" />
        <div>
          <h1 className="page-heading">播放统计</h1>
          <p className="page-subtitle">按真实播放会话查看趋势、热门作品和具体记录。</p>
        </div>
      </header>

      {filterError && <p className="rounded-xl bg-red-500/10 px-4 py-3 text-sm text-red-500" role="alert">{filterError}</p>}

      <section className="glass-panel space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <label className="text-xs text-ink-50">粒度<select className="input-base mt-1" value={grain} onChange={(event) => setGrain(event.target.value as PlaybackStatsQuery['grain'])}><option value="day">每日</option><option value="week">每周</option><option value="month">每月</option></select></label>
          <label className="text-xs text-ink-50">开始日期<input className="input-base mt-1" type="date" value={from} max={to} onChange={(event) => changeFrom(event.target.value)} /></label>
          <label className="text-xs text-ink-50">结束日期<input className="input-base mt-1" type="date" value={to} min={from} onChange={(event) => changeTo(event.target.value)} /></label>
          <label className="text-xs text-ink-50">账户<select className="input-base mt-1" value={userID} onChange={(event) => { setUserID(event.target.value); setPage(1) }}><option value="">全部账户</option>{users.map((user) => <option key={user.id} value={user.id}>{user.nickname || user.username}</option>)}</select></label>
          <label className="text-xs text-ink-50">媒体类型<select className="input-base mt-1" value={mediaType} onChange={(event) => { setMediaType(event.target.value as '' | 'movie' | 'tv'); setPage(1) }}><option value="">全部类型</option><option value="movie">电影</option><option value="tv">电视剧</option></select></label>
        </div>
        <fieldset>
          <legend className="text-xs text-ink-50">媒体库（不选表示全部）</legend>
          <div className="mt-2 flex max-h-32 flex-wrap gap-2 overflow-auto">
            {libraries.map((library) => <label key={library.id} className="badge-neutral cursor-pointer"><input type="checkbox" className="mr-2 accent-brand-500" checked={libraryIDs.includes(library.id)} onChange={(event) => { setLibraryIDs((current) => event.target.checked ? [...current, library.id] : current.filter((id) => id !== library.id)); setPage(1) }} />{library.name}</label>)}
          </div>
        </fieldset>
        <button type="button" className="btn-outline inline-flex items-center gap-2" onClick={() => void load()} disabled={loading}><RefreshCw size={16} className={loading ? 'animate-spin' : ''} />刷新</button>
      </section>

      <section className="glass-panel">
        <p className="text-sm text-ink-50">播放总次数</p>
        <p className="mt-1 text-4xl font-bold text-ink-600">{data?.total ?? 0}</p>
      </section>

      <section className="glass-panel space-y-4" aria-label="播放次数时间序列">
        <h2 className="text-lg font-semibold text-ink-600">播放趋势</h2>
        {loading && !data ? <p className="py-8 text-center text-ink-50">加载中...</p> : error ? <p className="py-8 text-center text-red-500" role="alert">{error}</p> : !data?.buckets.length ? <p className="py-8 text-center text-ink-50">当前条件下暂无播放记录。</p> : data.buckets.map((bucket) => (
          <div key={bucket.period} className="grid grid-cols-[6rem_minmax(0,1fr)_3rem] items-center gap-3 text-sm">
            <span className="text-ink-50">{bucket.period}</span>
            <div className="h-3 overflow-hidden rounded-full bg-gray-200" role="img" aria-label={`${bucket.period} 播放 ${bucket.count} 次`}><div className="h-full rounded-full bg-brand-500" style={{ width: `${Math.max(2, bucket.count / maxCount * 100)}%` }} /></div>
            <span className="text-right font-semibold text-ink-600">{bucket.count}</span>
          </div>
        ))}
      </section>

      <section className="glass-panel space-y-4" aria-labelledby="playback-ranking-title">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <div className="flex items-center gap-2"><Trophy size={20} className="text-gold-500" /><h2 id="playback-ranking-title" className="text-lg font-semibold text-ink-600">热门榜单</h2></div>
            <p className="mt-1 text-xs text-ink-50">电影按作品、电视剧按季统计 Top 10{data?.ranking.period ? ` · 周期 ${data.ranking.period}` : ''}</p>
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex rounded-xl bg-gray-200 p-1" role="group" aria-label="榜单周期">
              {(['day', 'week'] as const).map((value) => <button key={value} type="button" aria-pressed={rankGrain === value} className={`rounded-lg px-3 py-2 text-sm ${rankGrain === value ? 'bg-brand-600 font-semibold text-white shadow-sm' : 'text-ink-600'}`} onClick={() => setRankGrain(value)}>{value === 'day' ? '每日' : '每周'}</button>)}
            </div>
            <label className="text-xs text-ink-50">榜单日期<input type="date" className="input-base mt-1 w-auto" value={rankDate} min={from} max={to} onChange={(event) => setRankDate(event.target.value)} /></label>
          </div>
        </div>
        {loading && !data ? <p className="py-8 text-center text-ink-50">榜单加载中...</p> : error ? <p className="py-8 text-center text-red-500" role="alert">榜单加载失败。</p> : !data?.ranking.items.length ? <p className="py-8 text-center text-ink-50">该周期暂无热门内容。</p> : (
          <div className="space-y-2">
            {data.ranking.items.map((item, index) => (
              <div key={item.group_id} className="grid grid-cols-[2rem_3rem_minmax(0,1fr)_3.5rem] items-center gap-3 rounded-xl border border-[var(--app-border)] p-2.5">
                <span className={`text-center text-lg font-black ${index < 3 ? 'text-gold-500' : 'text-ink-50'}`}>{index + 1}</span>
                <StatsPoster src={item.poster_url} alt={rankTitle(item)} className="h-12 w-9 rounded-lg" />
                <div className="min-w-0"><p className="line-clamp-2 font-semibold leading-tight text-ink-600">{rankTitle(item)}</p><div className="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-200"><div className="h-full rounded-full bg-brand-500" style={{ width: `${Math.max(4, item.count / rankMax * 100)}%` }} /></div></div>
                <span className="text-right text-sm font-bold text-ink-600">{item.count} 次</span>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="glass-panel overflow-hidden p-0" aria-labelledby="playback-details-title">
        <div className="flex items-center justify-between border-b border-[var(--app-border)] p-4 sm:p-5">
          <div><h2 id="playback-details-title" className="text-lg font-semibold text-ink-600">播放明细</h2><p className="mt-1 text-xs text-ink-50">共 {data?.details.total ?? 0} 条真实播放记录</p></div>
          {loading && data && <span className="text-xs text-ink-50">更新中...</span>}
        </div>
        {loading && !data ? <p className="py-10 text-center text-ink-50">明细加载中...</p> : error ? <p className="py-10 text-center text-red-500" role="alert">明细加载失败。</p> : !data?.details.items.length ? <p className="py-10 text-center text-ink-50">当前条件下暂无播放明细。</p> : (
          <>
            <div className="hidden overflow-x-auto lg:block">
              <table className="data-table min-w-[760px]"><thead><tr><th>媒体</th><th>账户</th><th>媒体库</th><th>播放时间</th></tr></thead><tbody>{data.details.items.map((item) => <tr key={item.id}><td><div className="flex min-w-0 items-center gap-3"><StatsPoster src={item.poster_url} alt={detailTitle(item)} className="h-14 w-10 shrink-0 rounded-lg" /><div className="min-w-0">{item.media_available ? <Link to={`/media/${item.media_id}`} className="font-semibold text-ink-600 hover:text-brand-500">{detailTitle(item)}</Link> : <span className="font-semibold text-ink-600">{detailTitle(item)}</span>}{!item.media_available && <p className="mt-1 text-xs text-ink-50">媒体已不可用</p>}</div></div></td><td>{item.user_name}</td><td>{item.library_name}</td><td className="whitespace-nowrap">{new Date(item.played_at).toLocaleString()}</td></tr>)}</tbody></table>
            </div>
            <div className="divide-y divide-[var(--app-border)] lg:hidden">{data.details.items.map((item) => <article key={item.id} className="flex gap-3 p-4"><StatsPoster src={item.poster_url} alt={detailTitle(item)} className="h-20 w-14 shrink-0 rounded-lg" /><div className="min-w-0 flex-1">{item.media_available ? <Link to={`/media/${item.media_id}`} className="font-semibold text-ink-600 hover:text-brand-500">{detailTitle(item)}</Link> : <p className="font-semibold text-ink-600">{detailTitle(item)}</p>}<p className="mt-2 text-sm text-ink-50">{item.user_name} · {item.library_name}</p><p className="mt-1 text-xs text-ink-50">{new Date(item.played_at).toLocaleString()}{!item.media_available ? ' · 媒体已不可用' : ''}</p></div></article>)}</div>
          </>
        )}
        <div className="flex items-center justify-between border-t border-[var(--app-border)] px-4 py-3 text-sm text-ink-50">
          <button type="button" className="icon-btn" aria-label="上一页" disabled={page <= 1 || loading} onClick={() => setPage((current) => Math.max(1, current - 1))}><ChevronLeft size={18} /></button>
          <span>第 {page} / {pages} 页</span>
          <button type="button" className="icon-btn" aria-label="下一页" disabled={page >= pages || loading} onClick={() => setPage((current) => Math.min(pages, current + 1))}><ChevronRight size={18} /></button>
        </div>
      </section>
    </div>
  )
}
