import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Play } from 'lucide-react'

import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import { Select } from '../components/Select'
import type { Media } from '../types'
import type { HistoryItem } from '../types/history'
import { seriesTitleFromPath } from '../utils/groupSeries'
import { episodeIdentity, episodeLabel } from './seriesDetailModel'

type LibrarySeriesEpisodesProps = {
  loading: boolean
  selectedEpisodes: { season: number; episodes: Media[] }[]
  selectedSeason: number
  visibleEpisodes: Media[]
  selectedEpisodeID: string
  selectedVersionID?: string
  history: HistoryItem[]
  playbackFrom: string
  onSeasonChange: (season: number) => void
  onEpisodeSelect: (media: Media) => void
}

export function LibrarySeriesEpisodes({ loading, selectedEpisodes, selectedSeason, visibleEpisodes, selectedEpisodeID, selectedVersionID, history, playbackFrom, onSeasonChange, onEpisodeSelect }: LibrarySeriesEpisodesProps) {
  const stripRef = useRef<HTMLDivElement>(null)
  const seasonMediaID = loading ? '' : visibleEpisodes[0]?.id ?? ''
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
  }, [seasonMediaID, retry])
  const currentSeasonResult = seasonResult?.mediaID === seasonMediaID ? seasonResult : null
  const seasonMetadata = currentSeasonResult?.season?.season_num === selectedSeason ? currentSeasonResult.season : null
  const seasonLabel = visibleEpisodes.every((ep) => ep.episode_num <= 0) ? '未识别季集' : selectedSeason === 0 ? '特别篇' : `第 ${selectedSeason} 季`
  const seasonTitle = seasonMetadata?.title?.trim() ?? ''
  const genericTitle = /^(?:第\s*[0-9一二三四五六七八九十百零〇两]+\s*季|season[\s._-]*\d+|specials?|特别篇|特別篇)$/i.test(seasonTitle)
  const seasonHeading = seasonTitle && !genericTitle ? `${seasonLabel} · ${seasonTitle}` : seasonLabel
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
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex h-24 w-16 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-[var(--app-hover)] text-center text-xs text-[var(--app-muted)]">
          {seasonMetadata?.poster_url ? <img src={imageURL(seasonMetadata.poster_url, seasonMetadata.updated_at)} alt={`${seasonHeading}封面`} width={64} height={96} className="h-full w-full object-cover" referrerPolicy="no-referrer" /> : <span>{currentSeasonResult ? '暂无季封面' : '加载中…'}</span>}
        </div>
        <div className="mr-auto min-w-0 space-y-1">
          <h2 className="break-words font-display text-xl font-bold text-[var(--app-text)]">{seasonHeading}</h2>
          <p className="text-sm text-[var(--app-muted)]">可播放 {visibleEpisodes.length} {visibleEpisodes.some((ep) => ep.episode_num <= 0) ? '项' : '集'}</p>
          {currentSeasonResult?.failed && <button type="button" className="btn-ghost text-xs" onClick={() => { setSeasonResult(null); setRetry((value) => value + 1) }}>季资料加载失败，重试</button>}
        </div>
        {selectedEpisodes.length > 1 && <Select aria-label="选择季" className="btn-outline min-w-36" value={selectedSeason} onChange={(value) => onSeasonChange(Number(value))}>
          {selectedEpisodes.map(({ season, episodes }) => <option key={season} value={season}>{episodes.every((ep) => ep.episode_num <= 0) ? '未识别季集' : season === 0 ? '特别篇' : `第 ${season} 季`} · {episodes.length} {episodes.some((ep) => ep.episode_num <= 0) ? '项' : '集'}</option>)}
        </Select>}
        {visibleEpisodes.length > 12 && <Select aria-label="快速定位分集" className="btn-outline max-w-60" value={selectedEpisodeID} onChange={(value) => { const ep = visibleEpisodes.find((item) => episodeIdentity(item) === value); if (ep) onEpisodeSelect(ep) }}>
          {visibleEpisodes.map((ep) => <option key={ep.id} value={episodeIdentity(ep)}>{episodeLabel(ep)} · {episodeDisplayTitle(ep, visibleEpisodes)}</option>)}
        </Select>}
      </div>
      <div ref={stripRef} className="relative flex gap-3 overflow-x-auto pb-3" aria-label="分集列表">
        {visibleEpisodes.map((ep) => {
          const active = episodeIdentity(ep) === selectedEpisodeID
          const playID = active && selectedVersionID ? selectedVersionID : ep.id
          const progress = history.find((row) => row.metadata_id === ep.metadata_id)
          const percent = progress?.duration_ms ? Math.max(0, Math.min(100, progress.position_ms / progress.duration_ms * 100)) : 0
          const from = new URL(playbackFrom, window.location.origin)
          from.searchParams.set('season', String(selectedSeason))
          from.searchParams.set('episode', episodeIdentity(ep))
          from.searchParams.set('version', playID)
          return (
            <article key={episodeIdentity(ep)} className={`w-52 shrink-0 overflow-hidden rounded-2xl border bg-[var(--app-panel)] transition sm:w-60 ${active ? 'border-brand-500 ring-2 ring-brand-500/20' : 'border-[var(--app-border)]'}`}>
              <button type="button" aria-pressed={active} onClick={() => onEpisodeSelect(ep)} className="block w-full text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-500">
                <div className="relative flex aspect-video items-center justify-center bg-[var(--app-hover)] text-[var(--app-muted)]">
                  {ep.backdrop_url ? <img src={imageURL(ep.backdrop_url, ep.updated_at)} alt="" loading="lazy" className="h-full w-full object-cover" referrerPolicy="no-referrer" /> : <span>{episodeLabel(ep)}</span>}
                  <span className="absolute bottom-2 left-2 rounded-lg bg-black/65 px-2 py-1 text-xs font-bold text-white">{ep.episode_num > 0 ? `E${ep.episode_num}` : '文件'}</span>
                  {progress && <span className="absolute right-2 top-2 rounded-lg bg-black/65 px-2 py-1 text-xs text-white">{progress.completed ? '已看完' : '观看中'}</span>}
                  {progress && <div className="absolute inset-x-0 bottom-0 h-1 bg-black/20"><div className="h-full bg-brand-500" style={{ width: `${progress.completed ? 100 : percent}%` }} /></div>}
                </div>
                <div className="space-y-1 px-3 pt-3">
                  <p className="truncate text-sm font-bold text-[var(--app-text)]">{episodeDisplayTitle(ep, visibleEpisodes)}</p>
                  <p className="text-xs text-[var(--app-muted)]">{ep.release_date || '日期待补充'}{ep.duration_sec > 0 ? ` · ${Math.floor(ep.duration_sec / 60)} 分钟` : ''}</p>
                </div>
              </button>
              <div className="flex items-center justify-between px-3 py-2">
                <span className="text-xs text-brand-500">{active ? '当前选中' : '点击查看详情'}</span>
                <Link aria-label={`播放${episodeLabel(ep)}`} to={`/play/${playID}`} state={{ from: from.pathname + from.search }} className="btn-ghost min-h-11 !px-3"><Play size={16} aria-hidden="true" />播放</Link>
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}

function episodeDisplayTitle(ep: Media, siblings: Media[]): string {
  const title = ep.title?.trim()
  if (title && !looksLikeSeriesTitle(ep, title, siblings)) {
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
