import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronLeft,
  ChevronRight,
  Film,
  Filter,
  Flame,
  ListVideo,
  Play,
  RefreshCw,
  TrendingUp,
  Trophy,
} from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

import {
  adminAPI,
  type PlaybackStatsDetail,
  type PlaybackStatsQuery,
  type PlaybackStatsRankItem,
  type PlaybackStatsResult,
} from '../api/admin'
import { imageURL } from '../api/client'
import { libraryAPI } from '../api/library'
import { Select } from '../components/Select'
import type { Library, User } from '../types'

const DETAIL_PAGE_SIZE = 20
const SYSTEM_LABEL = { all: '全部体系', catalog: '普通媒体库', hongguo: '红果短剧', nfo: '非常规媒体库' }

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
    <div className={`${className} flex items-center justify-center text-ink-50`} style={{ background: 'var(--app-poster-empty)' }} aria-hidden="true">
      <Film size={20} />
    </div>
  )
}

/** 趋势图坐标标签：日/周粒度省略年份，月粒度原样展示。 */
function shortPeriod(period: string, grain: PlaybackStatsQuery['grain']): string {
  return grain === 'month' ? period : period.slice(5)
}

const GRAIN_LABEL: Record<PlaybackStatsQuery['grain'], string> = { day: '日', week: '周', month: '月' }

/** KPI 图标座的主题化配色（深浅主题均安全）。 */
const TONE_STYLE = {
  brand: { background: 'var(--app-brand-soft)', color: 'var(--app-accent)', boxShadow: 'inset 0 0 0 1px var(--app-brand-border), 0 0 18px var(--app-brand-soft)' },
  sage: { background: 'rgba(34, 211, 238, 0.1)', color: '#22d3ee', boxShadow: 'inset 0 0 0 1px rgba(34, 211, 238, 0.25)' },
  gold: { background: 'rgba(240, 179, 78, 0.12)', color: 'var(--app-gold)', boxShadow: 'inset 0 0 0 1px rgba(240, 179, 78, 0.3), 0 0 18px rgba(240, 179, 78, 0.1)' },
  neutral: { background: 'var(--app-hover)', color: 'var(--app-muted)', boxShadow: 'inset 0 0 0 1px var(--app-border)' },
} as const

/** 榜单前三名奖牌的渐变（金 / 银 / 铜）。 */
const MEDAL_CLASS = [
  'bg-gradient-to-br from-gold-300 to-gold-600 shadow-glow-gold',
  'bg-gradient-to-br from-slate-200 to-slate-400',
  'bg-gradient-to-br from-amber-300 to-amber-600',
]

export function PlaybackStatsPage() {
  const [params, setParams] = useSearchParams()
  const value = params.get('system')
  const system = value === 'catalog' || value === 'hongguo' || value === 'nfo' ? value : 'all'
  useEffect(() => {
    const values = params.getAll('system')
    if (values.length > 1 || (values.length === 1 && !Object.keys(SYSTEM_LABEL).includes(values[0]))) {
      const next = new URLSearchParams(params)
      next.set('system', system)
      setParams(next, { replace: true })
    }
  }, [params, setParams, system])
  return <div className="space-y-4">
    <label className="block text-sm text-ink-50">资料体系<Select className="input-base mt-1" value={system} onChange={(value) => { const next = new URLSearchParams(params); next.set('system', value); setParams(next) }}>{Object.entries(SYSTEM_LABEL).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select></label>
    <PlaybackStatsSystemPage key={system} system={system} />
  </div>
}

function PlaybackStatsSystemPage({ system }: { system: NonNullable<PlaybackStatsQuery['system']> }) {
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
        setLibraries(libraryRows.filter((library) => {
          const source = library.type === 'hongguo' ? 'hongguo' : library.type === 'nfo_movie' || library.type === 'nfo_tv' ? 'nfo' : 'catalog'
          return system === 'all' || source === system
        }))
      })
      .catch(() => setFilterError('筛选项加载失败。'))
  }, [system])

  const load = useCallback(() => {
    const currentRequest = ++requestID.current
    setLoading(true)
    setError('')
    return adminAPI.playbackStats({
      system,
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
  }, [from, grain, libraryIDs, mediaType, page, rankDate, rankGrain, to, userID, system])

  useEffect(() => { void load() }, [load])

  const maxCount = Math.max(1, ...(data?.buckets.map((bucket) => bucket.count) ?? []))
  const rankMax = Math.max(1, ...(data?.ranking.items.map((item) => item.count) ?? []))
  const pages = Math.max(1, Math.ceil((data?.details.total ?? 0) / DETAIL_PAGE_SIZE))
  const buckets = data?.buckets ?? []
  const peakBucket = buckets.reduce<(typeof buckets)[number] | null>(
    (peak, bucket) => (bucket.count > (peak?.count ?? 0) ? bucket : peak),
    null,
  )
  const rangeDays = Math.max(1, Math.round((new Date(to).getTime() - new Date(from).getTime()) / 86_400_000) + 1)
  const dailyAvg = ((data?.total ?? 0) / rangeDays).toFixed(1).replace(/\.0$/, '')
  // 柱状图横轴标签过密时稀疏展示，保持对齐只隐藏文字。
  const labelEvery = Math.max(1, Math.ceil(buckets.length / 10))
  const rankItems = data?.ranking.items ?? []
  const podium = rankItems.slice(0, 3)
  const rankRest = rankItems.slice(3)

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

  const kpis = [
    { icon: Play, tone: 'brand' as const, label: '播放总次数', value: (data?.total ?? 0).toLocaleString(), sub: `${from} ~ ${to}`, gradient: true },
    { icon: TrendingUp, tone: 'sage' as const, label: '日均播放', value: dailyAvg, sub: `统计区间共 ${rangeDays} 天`, gradient: false },
    { icon: Flame, tone: 'gold' as const, label: `单${GRAIN_LABEL[grain]}峰值`, value: (peakBucket?.count ?? 0).toLocaleString(), sub: peakBucket ? peakBucket.period : '暂无播放', gradient: false },
    { icon: ListVideo, tone: 'neutral' as const, label: '播放记录', value: (data?.details.total ?? 0).toLocaleString(), sub: '当前筛选下的明细条数', gradient: false },
  ]

  return (
    <div className="space-y-6">
      {filterError && <p className="rounded-xl bg-red-500/10 px-4 py-3 text-sm text-red-500" role="alert">{filterError}</p>}

      <section className="glass-panel space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Filter size={16} className="text-brand-500" />
            <h2 className="text-sm font-semibold text-ink-600">筛选条件</h2>
          </div>
          <button type="button" className="btn-outline inline-flex items-center gap-2 !px-3.5 !py-2" onClick={() => void load()} disabled={loading}><RefreshCw size={15} className={loading ? 'animate-spin' : ''} />刷新</button>
        </div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <label className="text-xs text-ink-50">粒度<Select className="input-base mt-1" value={grain} onChange={(value) => setGrain(value as PlaybackStatsQuery['grain'])}><option value="day">每日</option><option value="week">每周</option><option value="month">每月</option></Select></label>
          <label className="text-xs text-ink-50">开始日期<input className="input-base mt-1" type="date" value={from} max={to} onChange={(event) => changeFrom(event.target.value)} /></label>
          <label className="text-xs text-ink-50">结束日期<input className="input-base mt-1" type="date" value={to} min={from} onChange={(event) => changeTo(event.target.value)} /></label>
          <label className="text-xs text-ink-50">账户<Select className="input-base mt-1" value={userID} onChange={(value) => { setUserID(value); setPage(1) }}><option value="">全部账户</option>{users.map((user) => <option key={user.id} value={user.id}>{user.nickname || user.username}</option>)}</Select></label>
          <label className="text-xs text-ink-50">媒体类型<Select className="input-base mt-1" value={mediaType} onChange={(value) => { setMediaType(value as '' | 'movie' | 'tv'); setPage(1) }}><option value="">全部类型</option><option value="movie">电影</option><option value="tv">电视剧</option></Select></label>
        </div>
        <fieldset>
          <legend className="text-xs text-ink-50">媒体库（不选表示全部）</legend>
          <div className="mt-2 flex max-h-32 flex-wrap gap-2 overflow-auto">
            {libraries.map((library) => <label key={library.id} className="badge-neutral cursor-pointer"><input type="checkbox" className="mr-2 accent-brand-500" checked={libraryIDs.includes(library.id)} onChange={(event) => { setLibraryIDs((current) => event.target.checked ? [...current, library.id] : current.filter((id) => id !== library.id)); setPage(1) }} />{library.name}</label>)}
          </div>
        </fieldset>
      </section>

      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        {kpis.map(({ icon: Icon, tone, label, value, sub, gradient }) => (
          <div key={label} className="glass-panel flex items-center gap-3 !p-4 sm:gap-3.5 sm:!p-5">
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl sm:h-11 sm:w-11" style={TONE_STYLE[tone]}>
              <Icon size={19} />
            </span>
            <div className="min-w-0">
              <p className="text-xs text-ink-50">{label}</p>
              <p className={`mt-0.5 font-display text-xl font-extrabold tracking-tight sm:text-2xl ${gradient ? 'text-gradient-brand' : 'text-ink-600'}`}>{value}</p>
              <p className="mt-0.5 truncate text-2xs text-ink-50">{sub}</p>
            </div>
          </div>
        ))}
      </div>

      <section className="glass-panel" aria-label="播放次数时间序列">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-ink-600">播放趋势</h2>
          <span className="text-xs text-ink-50">按{GRAIN_LABEL[grain]}聚合</span>
        </div>
        {loading && !data ? <p className="py-8 text-center text-ink-50">加载中...</p> : error ? <p className="py-8 text-center text-red-500" role="alert">{error}</p> : !buckets.length ? <p className="py-8 text-center text-ink-50">当前条件下暂无播放记录。</p> : (
          <div className="mt-6 flex h-56 items-end gap-[3px] sm:gap-1.5">
            {buckets.map((bucket, index) => {
              const isPeak = bucket.count > 0 && bucket.count === maxCount
              return (
                <div key={bucket.period} className="group flex h-full min-w-0 flex-1 flex-col items-center justify-end">
                  <div className="relative w-full max-w-7" style={{ height: `${bucket.count === 0 ? 2 : Math.max(4, bucket.count / maxCount * 100)}%` }}>
                    <div className="pointer-events-none absolute bottom-full left-1/2 z-10 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg border px-2.5 py-1.5 text-2xs font-semibold opacity-0 shadow-elevated transition-opacity duration-200 group-hover:opacity-100" style={{ background: 'var(--app-panel)', borderColor: 'var(--app-border)', color: 'var(--app-text)' }}>
                      {bucket.period} · {bucket.count} 次
                    </div>
                    <div
                      role="img"
                      aria-label={`${bucket.period} 播放 ${bucket.count} 次`}
                      className={`h-full w-full rounded-t-md transition-all duration-300 ${bucket.count === 0 ? '' : isPeak ? 'bg-gradient-to-t from-gold-600 to-gold-400 shadow-glow-gold' : 'bg-gradient-to-t from-brand-700/80 to-brand-400 group-hover:from-brand-600 group-hover:to-brand-300 group-hover:shadow-glow-sm'}`}
                      style={bucket.count === 0 ? { background: 'var(--app-hover)' } : undefined}
                    />
                  </div>
                  <span className={`mt-2 whitespace-nowrap text-2xs ${index % labelEvery === 0 ? 'text-ink-50' : 'invisible'}`} aria-hidden={index % labelEvery !== 0}>{shortPeriod(bucket.period, grain)}</span>
                </div>
              )
            })}
          </div>
        )}
      </section>

      <section className="glass-panel space-y-5" aria-labelledby="playback-ranking-title">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <div className="flex items-center gap-2"><Trophy size={20} className="text-gold-500" /><h2 id="playback-ranking-title" className="text-lg font-semibold text-ink-600">热门榜单</h2></div>
            <p className="mt-1 text-xs text-ink-50">电影按作品、电视剧按季、红果按来源作品统计 Top 10{data?.ranking.period ? ` · 周期 ${data.ranking.period}` : ''}</p>
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex rounded-xl p-1" style={{ background: 'var(--app-hover)' }} role="group" aria-label="榜单周期">
              {(['day', 'week'] as const).map((value) => <button key={value} type="button" aria-pressed={rankGrain === value} className={`rounded-lg px-3 py-2 text-sm transition-all duration-200 ${rankGrain === value ? 'bg-brand-600 font-semibold text-white shadow-sm' : 'text-ink-600 hover:text-brand-500'}`} onClick={() => setRankGrain(value)}>{value === 'day' ? '每日' : '每周'}</button>)}
            </div>
            <label className="text-xs text-ink-50">榜单日期<input type="date" className="input-base mt-1 w-auto" value={rankDate} min={from} max={to} onChange={(event) => setRankDate(event.target.value)} /></label>
          </div>
        </div>
        {loading && !data ? <p className="py-8 text-center text-ink-50">榜单加载中...</p> : error ? <p className="py-8 text-center text-red-500" role="alert">榜单加载失败。</p> : !rankItems.length ? <p className="py-8 text-center text-ink-50">该周期暂无热门内容。</p> : (
          <>
            <div className={`grid gap-3 sm:grid-cols-3 ${podium.length === 1 ? 'sm:max-w-sm' : ''}`}>
              {podium.map((item, index) => {
                const rank = index + 1
                return (
                  <div key={`${item.system}:${item.group_id}`} className={`flex items-center gap-3 rounded-2xl border p-3 transition-colors hover:bg-[var(--app-hover)] ${rank === 1 ? 'shadow-glow-gold' : ''}`} style={{ borderColor: rank === 1 ? 'rgba(240, 179, 78, 0.45)' : 'var(--app-border)', background: rank === 1 ? 'rgba(240, 179, 78, 0.05)' : undefined }}>
                    <div className="relative shrink-0">
                      <StatsPoster src={item.poster_url} alt={rankTitle(item)} className="h-20 w-14 rounded-lg" />
                      <span className={`absolute -left-1.5 -top-1.5 flex h-6 w-6 items-center justify-center rounded-full text-2xs font-black text-white shadow-md ring-2 ring-[var(--app-panel)] ${MEDAL_CLASS[index]}`}>{rank}</span>
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="line-clamp-2 text-sm font-bold leading-snug text-ink-600">{rankTitle(item)}</p>
                      {system === 'all' && <p className="mt-1 text-xs text-ink-50">{SYSTEM_LABEL[item.system]}</p>}
                      <p className={`mt-1.5 text-xs font-extrabold ${rank === 1 ? 'text-gold-500' : 'text-ink-50'}`}>{item.count} 次播放</p>
                    </div>
                  </div>
                )
              })}
            </div>
            {rankRest.length > 0 && (
              <div className="divide-y divide-[var(--app-border)] overflow-hidden rounded-xl border border-[var(--app-border)]">
                {rankRest.map((item, index) => (
                  <div key={`${item.system}:${item.group_id}`} className="flex items-center gap-3 p-2.5 transition-colors hover:bg-[var(--app-hover)]">
                    <span className="w-6 shrink-0 text-center text-sm font-black text-ink-50">{index + 4}</span>
                    <StatsPoster src={item.poster_url} alt={rankTitle(item)} className="h-11 w-8 shrink-0 rounded-md" />
                    <div className="min-w-0 flex-1">
                      <p className="line-clamp-1 text-sm font-semibold text-ink-600">{rankTitle(item)}</p>
                      {system === 'all' && <p className="mt-1 text-xs text-ink-50">{SYSTEM_LABEL[item.system]}</p>}
                      <div className="mt-1.5 h-1 overflow-hidden rounded-full" style={{ background: 'var(--app-hover)' }}><div className="h-full rounded-full bg-gradient-to-r from-brand-600 to-brand-400" style={{ width: `${Math.max(4, item.count / rankMax * 100)}%` }} /></div>
                    </div>
                    <span className="shrink-0 text-sm font-bold text-ink-600">{item.count} 次</span>
                  </div>
                ))}
              </div>
            )}
          </>
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
              <table className="data-table min-w-[760px]"><thead><tr><th>媒体</th><th>账户</th><th>媒体库</th><th>播放时间</th></tr></thead><tbody>{data.details.items.map((item) => <tr key={`${item.system}:${item.id}`}><td><div className="flex min-w-0 items-center gap-3"><StatsPoster src={item.poster_url} alt={detailTitle(item)} className="h-14 w-10 shrink-0 rounded-lg" /><div className="min-w-0">{item.media_available ? <Link to={item.source_id ? `/discover?system=hongguo&id=${encodeURIComponent(item.source_id)}` : `/media/${item.media_id}`} className="font-semibold text-ink-600 hover:text-brand-500">{detailTitle(item)}</Link> : <span className="font-semibold text-ink-600">{detailTitle(item)}</span>}{!item.media_available && <p className="mt-1 text-xs text-ink-50">媒体已不可用</p>}</div></div></td><td>{item.user_name}</td><td>{item.library_name}{system === 'all' && <p className="mt-1 text-xs text-ink-50">{SYSTEM_LABEL[item.system]}</p>}</td><td className="whitespace-nowrap">{new Date(item.played_at).toLocaleString()}</td></tr>)}</tbody></table>
            </div>
            <div className="divide-y divide-[var(--app-border)] lg:hidden">{data.details.items.map((item) => <article key={`${item.system}:${item.id}`} className="flex gap-3 p-4"><StatsPoster src={item.poster_url} alt={detailTitle(item)} className="h-20 w-14 shrink-0 rounded-lg" /><div className="min-w-0 flex-1">{item.media_available ? <Link to={item.source_id ? `/discover?system=hongguo&id=${encodeURIComponent(item.source_id)}` : `/media/${item.media_id}`} className="font-semibold text-ink-600 hover:text-brand-500">{detailTitle(item)}</Link> : <p className="font-semibold text-ink-600">{detailTitle(item)}</p>}<p className="mt-2 text-sm text-ink-50">{item.user_name} · {item.library_name}{system === 'all' ? ` · ${SYSTEM_LABEL[item.system]}` : ''}</p><p className="mt-1 text-xs text-ink-50">{new Date(item.played_at).toLocaleString()}{!item.media_available ? ' · 媒体已不可用' : ''}</p></div></article>)}</div>
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
