import { Calendar, Circle, CircleAlert, CircleCheck, Clock, FileVideo, HardDrive, Heart, Monitor, Star } from 'lucide-react'
import { motion } from 'framer-motion'

import type { Media } from '../types'

type MediaDetailMetadataProps = {
  media: Media
  selectedMedia: Media
  favourite: boolean
  onToggleFavourite: () => void
}

const rise = (delay: number) => ({
  initial: { opacity: 0, y: 14 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.5, delay, ease: [0.21, 0.47, 0.32, 0.98] as const },
})

export function MediaDetailMetadata({ media, selectedMedia, favourite, onToggleFavourite }: MediaDetailMetadataProps) {
  const heading = media.title
  const seriesContext = media.series_title?.trim()
  const tmdbHref = tmdbURL(media)

  return (
    <>
      <div className="space-y-4">
        <motion.div {...rise(0)} className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-2">
            <h1 className="break-words text-pretty font-display text-[clamp(1.75rem,3.4vw,2.75rem)] font-extrabold tracking-tight text-gray-900 leading-[1.15]">
              {heading}
            </h1>
            {seriesContext && (
              <p className="text-sm font-semibold text-gray-500">
                {seriesContext}
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={onToggleFavourite}
            aria-pressed={favourite}
            aria-label={favourite ? '取消收藏' : '加入收藏'}
            className={
              'group btn-outline shrink-0 overflow-hidden !gap-0 !px-3 hover:!gap-2 focus-visible:!gap-2 ' +
              (favourite
                ? '!border-red-200 !bg-red-50 !text-red-600 hover:!bg-red-100/50'
                : 'hover:border-red-200 hover:text-red-600 hover:bg-red-50/50')
            }
          >
            <Heart size={18} fill={favourite ? 'currentColor' : 'none'} aria-hidden="true" />
            <span
              aria-hidden="true"
              className="max-w-0 translate-x-1 overflow-hidden whitespace-nowrap opacity-0 transition-all duration-300 ease-smooth group-hover:max-w-24 group-hover:translate-x-0 group-hover:opacity-100 group-focus-visible:max-w-24 group-focus-visible:translate-x-0 group-focus-visible:opacity-100"
            >
              {favourite ? '取消收藏' : '加入收藏'}
            </span>
          </button>
        </motion.div>

        <motion.div {...rise(0.08)} className="space-y-2.5 text-xs font-bold tracking-wide">
          <div className="flex flex-wrap items-center gap-2.5">
            <span className="badge-gold !px-3 !py-1.5 !text-xs shadow-glow-gold">
              <Star size={12} fill="currentColor" className="mr-1" />
              {media.rating > 0 ? media.rating.toFixed(1) : '-'}
            </span>
            {(media.release_date || media.year > 0) && (
              <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-text)] backdrop-blur">
                <Calendar size={13} className="text-brand-500" />
                <span>{media.release_date || `${media.year} 年`}</span>
              </span>
            )}
            <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-subtle)] backdrop-blur">
              <Clock size={13} aria-hidden="true" />
              {fmtDuration(media.duration_sec)}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2.5">
            {media.width > 0 && (
              <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] px-3 py-1.5 uppercase text-[var(--app-brand-text)] backdrop-blur">
                <Monitor size={13} aria-hidden="true" />
                {media.width} × {media.height}
              </span>
            )}
            <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-subtle)] backdrop-blur">
              <HardDrive size={13} aria-hidden="true" />
              {fmtSize(media.size_bytes)}
            </span>
            {media.container && (
              <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 font-mono text-[10px] uppercase text-[var(--app-subtle)] backdrop-blur">
                <FileVideo size={13} aria-hidden="true" />
                {media.container}
              </span>
            )}
          </div>
          {(tmdbHref || media.douban_id) && (
            <div className="flex flex-wrap items-center gap-2.5">
              {tmdbHref && (
                <ProviderLink
                  href={tmdbHref}
                  label="TMDb"
                  iconSrc="/brand/tmdb.svg"
                  status={providerStatus(media.tmdb_status, media.tmdb_snapshot)}
                />
              )}
              {media.douban_id && (
                <ProviderLink
                  href={`https://movie.douban.com/subject/${encodeURIComponent(media.douban_id)}/`}
                  label="豆瓣"
                  iconSrc="/brand/douban.svg"
                  status={providerStatus(media.douban_status, media.douban_snapshot)}
                />
              )}
            </div>
          )}
        </motion.div>
      </div>

      {media.overview && (
        <motion.div {...rise(0.16)} className="glass-panel !rounded-2xl !p-5 sm:!p-6 space-y-2.5">
          <h3 className="text-xs font-bold uppercase tracking-[0.2em] text-brand-500">剧情简介</h3>
          <p className="max-w-3xl text-[15px] leading-7 text-[var(--app-subtle)] font-medium">
            {media.overview}
          </p>
        </motion.div>
      )}

      <motion.div {...rise(0.22)} className="space-y-4">
        <MetadataTags label="类型流派" values={parseCSV(media.genres)} primary />
        <div className="grid gap-4 sm:grid-cols-2">
          <MetadataTags label="国家/地区" values={localizedCSV(media.countries, 'region')} />
          <MetadataTags label="语言" values={localizedCSV(media.languages, 'language')} />
        </div>
        <div className="space-y-3 rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)]/50 p-4 text-xs">
          <div className="flex min-w-0 gap-3">
            <span className="w-16 shrink-0 font-bold uppercase tracking-wider text-[var(--app-muted)]">Media ID</span>
            <span className="min-w-0 break-all font-mono text-[var(--app-subtle)]">{selectedMedia.id}</span>
          </div>
          <div className="flex min-w-0 gap-3">
            <span className="w-16 shrink-0 font-bold uppercase tracking-wider text-[var(--app-muted)]">本地路径</span>
            <span className="min-w-0 break-all font-mono text-[var(--app-subtle)]">{selectedMedia.path}</span>
          </div>
        </div>
      </motion.div>
    </>
  )
}

type ProviderStatus = 'missing' | 'partial' | 'degraded' | 'complete'

const providerStatusLabels: Record<ProviderStatus, string> = {
  missing: '本地未缓存',
  partial: '本地数据不完整',
  degraded: '豆瓣接口受限，当前为降级数据',
  complete: '本地详情和图片完整',
}

function providerStatus(status: ProviderStatus | undefined, snapshot: boolean | undefined): ProviderStatus {
  return status ?? (snapshot ? 'partial' : 'missing')
}

function ProviderLink({ href, label, iconSrc, status }: { href: string; label: string; iconSrc: string; status: ProviderStatus }) {
  const statusLabel = providerStatusLabels[status]
  const warning = status === 'partial' || status === 'degraded'
  const StatusIcon = status === 'complete' ? CircleCheck : warning ? CircleAlert : Circle
  const statusClass = status === 'complete' ? 'text-emerald-600' : warning ? 'text-amber-600' : 'text-[var(--app-muted)]'
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={`${label}：${statusLabel}`}
      aria-label={`${label}：${statusLabel}，点击打开`}
      className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-2.5 py-1.5 text-[var(--app-text)] backdrop-blur hover:border-[var(--app-brand-border)] hover:text-[var(--app-brand-text)]"
    >
      <img src={iconSrc} alt="" aria-hidden="true" className="h-4 w-auto shrink-0" />
      <StatusIcon size={13} aria-hidden="true" className={statusClass} />
      <span className="sr-only">{statusLabel}</span>
    </a>
  )
}

function tmdbURL(media: Media): string | null {
  if (!media.tmdb_id) return null
  const base = 'https://www.themoviedb.org'
  if (media.metadata_kind === 'movie') return `${base}/movie/${media.tmdb_id}`
  if (media.metadata_kind === 'series') return `${base}/tv/${media.tmdb_id}`
  if (!media.series_tmdb_id) return null
  if (media.metadata_kind === 'season') return `${base}/tv/${media.series_tmdb_id}/season/${media.season_num}`
  if (media.metadata_kind === 'episode' && media.episode_num > 0) {
    return `${base}/tv/${media.series_tmdb_id}/season/${media.season_num}/episode/${media.episode_num}`
  }
  return null
}

function MetadataTags({ label, values, primary = false }: { label: string; values: string[]; primary?: boolean }) {
  if (values.length === 0) return null
  const tagClass = primary
    ? 'rounded-full bg-[var(--app-brand-soft)] text-[var(--app-brand-text)] border border-[var(--app-brand-border)] px-3 py-1 text-2xs font-bold uppercase tracking-wider'
    : 'rounded-xl bg-[var(--app-hover)] text-[var(--app-muted)] border border-[var(--app-border)] px-2.5 py-1 text-2xs font-semibold'
  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="w-16 whitespace-nowrap text-xs font-bold uppercase tracking-wider text-[var(--app-muted)]">{label}</span>
      <div className="flex flex-wrap gap-2">
        {values.map((value) => (
          <span key={value} className={tagClass}>
            {value}
          </span>
        ))}
      </div>
    </div>
  )
}

function fmtDuration(sec: number): string {
  if (!sec || sec <= 0) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

function fmtSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(2)} ${units[i]}`
}

function parseCSV(s?: string): string[] {
  if (!s) return []
  return s.split(',').map((x) => x.trim()).filter(Boolean)
}

const chineseNames = typeof Intl.DisplayNames === 'function'
  ? {
      region: new Intl.DisplayNames(['zh-CN'], { type: 'region' }),
      language: new Intl.DisplayNames(['zh-CN'], { type: 'language' }),
    }
  : null

function localizedCSV(s: string | undefined, type: 'region' | 'language'): string[] {
  return parseCSV(s).map((value) => {
    const code = type === 'region' ? value.toUpperCase() : value
    const isCode = type === 'region'
      ? /^(?:[A-Z]{2}|\d{3})$/.test(code)
      : /^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$/.test(code)
    if (!isCode || !chineseNames) return value
    try {
      return chineseNames[type].of(code) || value
    } catch {
      return value
    }
  })
}
