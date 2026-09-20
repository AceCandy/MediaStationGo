import { useEffect, useRef, useState } from 'react'
import { Calendar, CircleAlert, CircleCheck, Star } from 'lucide-react'

import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import { Select } from '../components/Select'
import type { Media } from '../types'
import type { HistoryItem } from '../types/history'
import { seriesTitleFromPath } from '../utils/groupSeries'
import { episodeIdentity, episodeLabel } from './seriesDetailModel'
import { LibrarySeasonActions } from './LibrarySeasonActions'

type LibrarySeriesEpisodesProps = {
  loading: boolean
  selectedEpisodes: { season: number; episodes: Media[] }[]
  selectedSeason: number
  visibleEpisodes: Media[]
  selectedEpisodeID: string
  history: HistoryItem[]
  isAdmin: boolean
  onChanged: () => void
  onSeasonChange: (season: number) => void
  onEpisodeSelect: (media: Media) => void
}

function SeasonCard({ selectedSeason, visibleEpisodes, active, current = false, onSeasonChange, isAdmin = false, onChanged, revision = 0 }: Pick<LibrarySeriesEpisodesProps, 'selectedSeason' | 'visibleEpisodes' | 'onSeasonChange'> & { active: boolean; current?: boolean; isAdmin?: boolean; onChanged?: () => void; revision?: number }) {
  const seasonMediaID = visibleEpisodes[0]?.id ?? ''
  const [seasonResult, setSeasonResult] = useState<{ mediaID: string; season: Media | null; failed: boolean } | null>(null)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!seasonMediaID) return
    let cancelled = false
    mediaAPI.season(seasonMediaID).then((season) => {
      if (!cancelled) setSeasonResult({ mediaID: seasonMediaID, season, failed: false })
    }).catch(() => {
      if (!cancelled) setSeasonResult({ mediaID: seasonMediaID, season: null, failed: true })
    })
    return () => { cancelled = true }
  }, [seasonMediaID, retry, revision])
  const currentSeasonResult = seasonResult?.mediaID === seasonMediaID ? seasonResult : null
  const seasonMetadata = currentSeasonResult?.season?.season_num === selectedSeason ? currentSeasonResult.season : null
  const seasonLabel = visibleEpisodes.every((ep) => ep.episode_num <= 0) ? '未识别季集' : selectedSeason === 0 ? '特别篇' : `第 ${selectedSeason} 季`
  const seasonTitle = seasonMetadata?.title?.trim() ?? ''
  const genericTitle = /^(?:第\s*[0-9一二三四五六七八九十百零〇两]+\s*季|season[\s._-]*\d+|specials?|特别篇|特別篇)$/i.test(seasonTitle)
  const seasonHeading = seasonTitle && !genericTitle ? `${seasonLabel} · ${seasonTitle}` : seasonLabel
  const tmdbStatus = seasonMetadata?.tmdb_status === 'complete' ? '本地详情和图片完整' : seasonMetadata?.tmdb_status === 'partial' ? '本地数据不完整' : seasonMetadata?.tmdb_id ? '本地未缓存' : '未关联 TMDB 季信息'
  const poster = seasonMetadata?.poster_url ? <img src={imageURL(seasonMetadata.poster_url, seasonMetadata.updated_at)} alt="" loading="lazy" className="h-full w-full object-cover" referrerPolicy="no-referrer" /> : <span className="p-2 text-center text-xs text-[var(--app-muted)]">{currentSeasonResult ? '暂无季封面' : '加载中…'}</span>
  const retryButton = currentSeasonResult?.failed && <button type="button" className="btn-ghost min-h-11 text-xs" onClick={() => { setSeasonResult(null); setRetry((value) => value + 1) }}>季资料加载失败，重试</button>
  if (!current) return (
    <div className="relative w-28 shrink-0 -ml-12 first:ml-0 transition-transform duration-200 hover:z-20 hover:-translate-y-1 focus-within:z-20 focus-within:-translate-y-1 motion-reduce:transform-none motion-reduce:transition-none">
      <button type="button" aria-label={seasonHeading} aria-pressed={active} onClick={() => { if (!active) onSeasonChange(selectedSeason) }} className={`relative flex h-[10.5rem] w-full items-center justify-center overflow-hidden rounded-xl border bg-[var(--app-hover)] shadow-lg transition-shadow hover:border-brand-400 hover:shadow-[0_0_20px_rgb(139_92_246/0.5)] focus-visible:border-brand-400 focus-visible:shadow-[0_0_20px_rgb(139_92_246/0.5)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-500 ${active ? 'border-brand-500 ring-2 ring-brand-500/30' : 'border-[var(--app-border)]'}`}>
        {poster}
        <span className="absolute bottom-2 left-2 rounded-md bg-black/70 px-2 py-1 text-xs font-bold text-white">S{selectedSeason}</span>
      </button>
      {retryButton}
    </div>
  )
  const StatusIcon = seasonMetadata?.tmdb_status === 'complete' ? CircleCheck : CircleAlert
  const providerBadge = <><img src="/brand/tmdb.svg" alt="TMDB" className="h-4 w-auto" /><StatusIcon size={14} aria-hidden="true" className={seasonMetadata?.tmdb_status === 'complete' ? 'text-emerald-600' : 'text-amber-600'} /><span className="sr-only">{tmdbStatus}</span></>
  return (
    <>
      <div aria-label="当前季" className="order-1 flex min-w-0 gap-4 rounded-2xl border border-brand-500 bg-[var(--app-panel)] p-3 ring-2 ring-brand-500/20 sm:p-4">
        <div className="flex h-36 w-24 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-[var(--app-hover)] sm:h-[10.5rem] sm:w-28">{poster}</div>
        <div className="min-w-0 flex-1 space-y-3 py-1">
          <h2 className="break-words font-display text-xl font-bold text-[var(--app-text)] sm:text-2xl">{seasonHeading}</h2>
          <p className="text-sm text-[var(--app-muted)]">可播放 {visibleEpisodes.length} {visibleEpisodes.some((ep) => ep.episode_num <= 0) ? '项' : '集'}</p>
          {seasonMetadata && <div className="flex flex-wrap items-center gap-2 text-xs text-[var(--app-subtle)]">
            <span className="inline-flex items-center gap-1 rounded-lg bg-[var(--app-hover)] px-2 py-1.5" aria-label={`评分 ${seasonMetadata.rating > 0 ? seasonMetadata.rating.toFixed(1) : '暂无'}`}><Star size={14} className="text-gold-500" aria-hidden="true" />{seasonMetadata.rating > 0 ? seasonMetadata.rating.toFixed(1) : '暂无'}</span>
            {seasonMetadata.release_date && <span className="inline-flex items-center gap-1 rounded-lg bg-[var(--app-hover)] px-2 py-1.5" title="首播时间"><Calendar size={14} aria-hidden="true" />{seasonMetadata.release_date}</span>}
            {seasonMetadata.series_tmdb_id ? <a className="inline-flex min-h-9 items-center gap-1 rounded-lg border border-[var(--app-border)] px-2 hover:border-brand-500" title={`TMDB：${tmdbStatus}`} aria-label={`TMDB：${tmdbStatus}，查看季页面`} href={`https://www.themoviedb.org/tv/${seasonMetadata.series_tmdb_id}/season/${selectedSeason}`} target="_blank" rel="noopener noreferrer">{providerBadge}</a> : <span className="inline-flex items-center gap-1 px-2" title={`TMDB：${tmdbStatus}`}>{providerBadge}</span>}
          </div>}
          {seasonMetadata?.genres && <p className="text-xs text-[var(--app-muted)]">{seasonMetadata.genres}</p>}
          {retryButton}
          {isAdmin && seasonMetadata && <LibrarySeasonActions key={seasonMediaID} season={seasonMetadata} mediaID={seasonMediaID} episodes={visibleEpisodes} onChanged={async () => {
            const season = await mediaAPI.season(seasonMediaID)
            setSeasonResult({ mediaID: seasonMediaID, season, failed: false })
            onChanged?.()
          }} />}
        </div>
      </div>
      <section aria-label="季资料" className="order-3 min-w-0 md:col-span-2">
        <p className="text-sm leading-6 text-[var(--app-subtle)]">{seasonMetadata?.overview || (currentSeasonResult ? '暂无季介绍' : '正在加载季资料…')}</p>
      </section>
    </>
  )
}

export function LibrarySeriesEpisodes({ loading, selectedEpisodes, selectedSeason, visibleEpisodes, selectedEpisodeID, history, isAdmin, onChanged, onSeasonChange, onEpisodeSelect }: LibrarySeriesEpisodesProps) {
  const stripRef = useRef<HTMLDivElement>(null)
  const [posterRevision, setPosterRevision] = useState(0)
  useEffect(() => {
    const strip = stripRef.current
    const selected = strip?.querySelector<HTMLElement>('[aria-pressed="true"]')
    if (strip && selected) {
      const left = selected.offsetLeft - strip.offsetLeft
      if (left < strip.scrollLeft || left + selected.offsetWidth > strip.scrollLeft + strip.clientWidth) {
        strip.scrollTo({ left: Math.max(0, left - 8), behavior: 'instant' })
      }
    }
  }, [selectedEpisodeID])
  if (loading) return <p role="status" className="p-6 text-sm text-[var(--app-muted)]">正在加载季与分集…</p>
  if (selectedEpisodes.length === 0) return <p className="p-6 text-sm text-[var(--app-muted)]">暂无可播放分集</p>
  return (
    <section className="min-w-0 space-y-4" aria-label="季与分集">
      <div className="grid min-w-0 items-center gap-4 md:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] lg:grid-cols-[minmax(0,28rem)_minmax(0,1fr)]">
        <SeasonCard selectedSeason={selectedSeason} visibleEpisodes={visibleEpisodes} active current isAdmin={isAdmin} onChanged={() => { setPosterRevision(value => value + 1); onChanged() }} onSeasonChange={onSeasonChange} />
        {selectedEpisodes.length > 1 && <div className="isolate order-2 flex min-w-0 overflow-x-auto px-3 py-4" aria-label="完整季列表">
          {selectedEpisodes.map(({ season, episodes }) => (
            <SeasonCard key={season} selectedSeason={season} visibleEpisodes={episodes} active={season === selectedSeason} revision={posterRevision} onSeasonChange={onSeasonChange} />
          ))}
        </div>}
      </div>
      {visibleEpisodes.length > 12 && <Select aria-label="快速定位分集" className="btn-outline max-w-60" value={selectedEpisodeID} onChange={(value) => { const ep = visibleEpisodes.find((item) => episodeIdentity(item) === value); if (ep) onEpisodeSelect(ep) }}>
        {visibleEpisodes.map((ep) => {
          const label = episodeLabel(ep)
          const title = episodeDisplayTitle(ep, visibleEpisodes)
          return <option key={ep.id} value={episodeIdentity(ep)}>{title === label ? label : `${label} · ${title}`}</option>
        })}
      </Select>}
      <div ref={stripRef} className="relative flex gap-3 overflow-x-auto pb-3" aria-label="分集列表">
        {visibleEpisodes.map((ep) => {
          const active = episodeIdentity(ep) === selectedEpisodeID
          const progress = history.find((row) => row.metadata_id === episodeIdentity(ep))
          const percent = progress?.duration_ms ? Math.max(0, Math.min(100, progress.position_ms / progress.duration_ms * 100)) : 0
          return (
            <article key={episodeIdentity(ep)} className={`w-52 shrink-0 overflow-hidden rounded-2xl border bg-[var(--app-panel)] transition sm:w-60 ${active ? 'border-brand-500 ring-2 ring-brand-500/20' : 'border-[var(--app-border)]'}`}>
              <button type="button" aria-pressed={active} onClick={() => onEpisodeSelect(ep)} className="block w-full text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-500">
                <div className="relative flex aspect-video items-center justify-center bg-[var(--app-hover)] text-[var(--app-muted)]">
                  {ep.backdrop_url ? <img src={imageURL(ep.backdrop_url, ep.updated_at)} alt="" loading="lazy" className="h-full w-full object-cover" referrerPolicy="no-referrer" /> : <span>{episodeLabel(ep)}</span>}
                  {progress && <span className="absolute right-2 top-2 rounded-lg bg-black/65 px-2 py-1 text-xs text-white">{progress.completed ? '已看完' : '观看中'}</span>}
                  {progress && <div className="absolute inset-x-0 bottom-0 h-1 bg-black/20"><div className="h-full bg-brand-500" style={{ width: `${progress.completed ? 100 : percent}%` }} /></div>}
                </div>
                <div className="space-y-1 px-3 py-2">
                  <p className="truncate text-sm font-bold text-[var(--app-text)]" title={episodeDisplayTitle(ep, visibleEpisodes)}>{ep.episode_num > 0 ? `S${selectedSeason}E${ep.episode_num}:` : ''}{episodeDisplayTitle(ep, visibleEpisodes)}</p>
                  <p className="text-xs text-[var(--app-muted)]">{ep.release_date || '播出时间待补充'}</p>
                </div>
              </button>
            </article>
          )
        })}
      </div>
    </section>
  )
}

function episodeDisplayTitle(ep: Media, siblings: Media[]): string {
  const title = ep.title?.trim()
  const genericTitle = /^(?:第\s*[0-9一二三四五六七八九十百零〇两]+\s*集|episode[\s._-]*\d+)$/i.test(title ?? '')
  if (title && !genericTitle && !looksLikeSeriesTitle(ep, title, siblings)) {
    return title
  }

  return ep.episode_num > 0 ? `第 ${ep.episode_num} 集` : title || '未命名'
}

function looksLikeSeriesTitle(ep: Media, title: string, siblings: Media[]): boolean {
  const normalized = normalizeEpisodeTitle(title)
  if (!normalized) return true
  if (ep.series_title && normalizeEpisodeTitle(ep.series_title) === normalized) return true
  if (ep.original_name && normalizeEpisodeTitle(ep.original_name) === normalized) return true
  const pathTitle = seriesTitleFromPath(ep.path)
  if (pathTitle && normalizeEpisodeTitle(pathTitle) === normalized) return true

  const siblingTitles = new Set(
    siblings
      .map((item) => normalizeEpisodeTitle(item.title))
      .filter(Boolean),
  )
  return siblingTitles.size === 1 && siblingTitles.has(normalized) && siblings.length > 1
}

function normalizeEpisodeTitle(value?: string): string {
  return (value ?? '')
    .toLowerCase()
    .replace(/\s*\((?:19|20)\d{2}\)\s*/g, ' ')
    .replace(/\s*\{(?:tmdb|tmdbid|douban|bangumi|bgm|thetvdb|tvdb)[\s:=#-]*[a-z0-9_-]+\}\s*/g, ' ')
    .replace(/[\s._-]+/g, ' ')
    .trim()
}
