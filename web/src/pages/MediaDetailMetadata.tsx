import { Calendar, Circle, CircleAlert, CircleCheck, Clock, FileVideo, HardDrive, Heart, Monitor, Star, Trash2, Unlink } from 'lucide-react'
import { motion } from 'framer-motion'
import { Fragment, useEffect, useState, type ReactNode } from 'react'

import { mediaAPI, type STRMDeleteTarget } from '../api/library'
import { STRMDeleteDialog } from '../components/STRMDeleteDialog'
import type { Media } from '../types'

type MediaDetailMetadataProps = {
  media: Media
  selectedMedia?: Media
  scope?: 'series' | 'episode'
  isAdmin: boolean
  favourite?: boolean
  onToggleFavourite?: () => void
  onMetadataEdit: () => void
  actions?: ReactNode
}

const rise = (delay: number) => ({
  initial: { opacity: 0, y: 14 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.5, delay, ease: [0.21, 0.47, 0.32, 0.98] as const },
})

export function MediaDetailMetadata({ media, selectedMedia, scope, isAdmin, favourite, onToggleFavourite, onMetadataEdit, actions }: MediaDetailMetadataProps) {
  const Details = scope ? 'div' : Fragment
  const isEpisode = scope === 'episode' || media.metadata_kind === 'episode'
  const heading = media.title
  const seriesContext = media.series_title?.trim()
  const tmdbHref = tmdbURL(media)
  const missingTMDbEpisode = isEpisode && (media.tmdb_id > 0 || (media.series_tmdb_id ?? 0) > 0) &&
    providerStatus(media.tmdb_status, media.tmdb_snapshot) === 'missing'
  const [strmTarget, setSTRMTarget] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<STRMDeleteTarget | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)

  useEffect(() => {
    let cancelled = false
    setSTRMTarget('')
    setDeleteTarget(null)
    setDeleteOpen(false)
    if (selectedMedia?.path.toLowerCase().endsWith('.strm')) {
      mediaAPI.getSTRMTarget(selectedMedia.id)
        .then((target) => { if (!cancelled) setSTRMTarget(target) })
        .catch(() => undefined)
      if (isAdmin) {
        mediaAPI.getSTRMDeleteTarget(selectedMedia.id)
          .then((target) => { if (!cancelled) setDeleteTarget(target) })
          .catch(() => undefined)
      }
    }
    return () => { cancelled = true }
  }, [isAdmin, selectedMedia?.id, selectedMedia?.path])

  return (
    <>
      <div className="space-y-4">
        <motion.div {...rise(0)} className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-2">
            {scope === 'episode' ? (
              <h2 className="break-words font-display text-xl font-extrabold text-[var(--app-text)]">{heading}</h2>
            ) : (
              <h1 className="break-words text-pretty font-display text-[clamp(1.75rem,3.4vw,2.75rem)] font-extrabold tracking-tight text-[var(--app-text)] leading-[1.15]">{heading}</h1>
            )}
            {!scope && seriesContext && (
              <p className="text-sm font-semibold text-gray-500">
                {seriesContext}
              </p>
            )}
          </div>
          {onToggleFavourite && (media.metadata_kind === 'movie' || scope === 'series') && <button
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
          </button>}
        </motion.div>

        <motion.div {...rise(0.08)} className="space-y-2.5 text-xs font-bold tracking-wide">
          <div className="flex flex-wrap items-center gap-2.5">
            {!isEpisode && <span className="badge-gold !px-3 !py-1.5 !text-xs shadow-glow-gold">
              <Star size={12} fill="currentColor" className="mr-1" />
              {media.rating > 0 ? media.rating.toFixed(1) : '-'}
            </span>}
            {(media.release_date || media.year > 0) && (
              <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-text)] backdrop-blur">
                <Calendar size={13} className="text-brand-500" />
                <span>{media.release_date || `${media.year} 年`}</span>
              </span>
            )}
            {scope !== 'series' && <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-subtle)] backdrop-blur">
              <Clock size={13} aria-hidden="true" />
              {fmtDuration(media.duration_sec)}
            </span>}
          </div>
          {scope !== 'series' && <div className="flex flex-wrap items-center gap-2.5">
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
          </div>}
          {(tmdbHref || missingTMDbEpisode || !isEpisode) && <div className="flex flex-wrap items-center gap-2.5">
            {(tmdbHref || missingTMDbEpisode) && (
              <ProviderBadge
                href={tmdbHref ?? undefined}
                label="TMDb"
                iconSrc="/brand/tmdb.svg"
                status={missingTMDbEpisode ? 'episode_missing' : providerStatus(media.tmdb_status, media.tmdb_snapshot)}
              />
            )}
            {!isEpisode && <ProviderBadge
              href={media.douban_id ? `https://movie.douban.com/subject/${encodeURIComponent(media.douban_id)}/` : undefined}
              onClick={!media.douban_id && isAdmin ? onMetadataEdit : undefined}
              label="豆瓣"
              iconSrc="/brand/douban.svg"
              status={media.douban_id ? providerStatus(media.douban_status, media.douban_snapshot) : 'unlinked'}
            />}
          </div>}
        </motion.div>
      </div>

      {actions}
      <Details {...(scope ? { className: 'space-y-4 text-[var(--app-subtle)]' } : {})}>
      {media.overview && (
        <motion.div {...rise(0.16)} className={scope ? 'space-y-2.5' : 'glass-panel !rounded-2xl !p-5 sm:!p-6 space-y-2.5'}>
          <h3 className="text-xs font-bold uppercase tracking-[0.2em] text-brand-500">剧情简介</h3>
          <p className="max-w-3xl text-[15px] leading-7 text-[var(--app-subtle)] font-medium">
            {media.overview}
          </p>
        </motion.div>
      )}

      <motion.div {...rise(0.22)} className="space-y-4">
        {scope !== 'episode' && <MetadataTags label="类型流派" values={parseCSV(media.genres)} primary />}
        {scope !== 'episode' && <div className="grid gap-4 sm:grid-cols-2">
          <MetadataTags label="国家/地区" values={localizedCSV(media.countries, 'region')} />
          <MetadataTags label="语言" values={localizedCSV(media.languages, 'language')} />
        </div>}
        {selectedMedia && <div className="space-y-3 rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)]/50 p-4 text-xs">
          <div className="flex min-w-0 gap-3">
            <span className="w-16 shrink-0 font-bold uppercase tracking-wider text-[var(--app-muted)]">Media ID</span>
            <span className="min-w-0 break-all font-mono text-[var(--app-subtle)]">{selectedMedia.id}</span>
          </div>
          <div className="flex min-w-0 gap-3">
            <span className="w-16 shrink-0 font-bold uppercase tracking-wider text-[var(--app-muted)]">本地路径</span>
            <span className="min-w-0 break-all font-mono text-[var(--app-subtle)]">{selectedMedia.path}</span>
          </div>
          {strmTarget && (
            <div className="flex min-w-0 gap-3">
              <span className="w-16 shrink-0 font-bold uppercase tracking-wider text-[var(--app-muted)]">STRM 路径</span>
              <span className="min-w-0 flex-1 break-all font-mono text-[var(--app-subtle)]">{strmTarget}</span>
              {deleteTarget && (
                <button
                  type="button"
                  className="btn-danger shrink-0 !px-2.5 !py-1.5"
                  onClick={() => setDeleteOpen(true)}
                  aria-label="删除 STRM 本地目标"
                  title="删除 STRM 本地目标"
                >
                  <Trash2 size={14} aria-hidden="true" />
                  删除
                </button>
              )}
            </div>
          )}
        </div>}
      </motion.div>
      </Details>

      {deleteOpen && deleteTarget && selectedMedia && (
        <STRMDeleteDialog
          key={selectedMedia.id}
          mediaID={selectedMedia.id}
          target={deleteTarget}
          onClose={() => setDeleteOpen(false)}
          onDeleted={() => { setDeleteOpen(false); setDeleteTarget(null) }}
        />
      )}
    </>
  )
}

type ProviderStatus = 'unlinked' | 'missing' | 'partial' | 'degraded' | 'complete' | 'episode_missing'

const providerStatusLabels: Record<ProviderStatus, string> = {
  unlinked: '没有豆瓣信息',
  missing: '本地未缓存',
  episode_missing: '未获取到 TMDB 单集信息',
  partial: '本地数据不完整',
  degraded: '豆瓣接口受限，当前为降级数据',
  complete: '本地详情和图片完整',
}

function providerStatus(status: ProviderStatus | undefined, snapshot: boolean | undefined): ProviderStatus {
  return status ?? (snapshot ? 'partial' : 'missing')
}

function ProviderBadge({ href, onClick, label, iconSrc, status }: { href?: string; onClick?: () => void; label: string; iconSrc: string; status: ProviderStatus }) {
  const statusLabel = providerStatusLabels[status]
  const missingEpisode = status === 'episode_missing'
  const warning = status === 'partial' || status === 'degraded'
  const StatusIcon = missingEpisode ? CircleAlert : status === 'unlinked' ? Unlink : status === 'complete' ? CircleCheck : warning ? CircleAlert : Circle
  const statusClass = missingEpisode ? 'text-red-700' : status === 'complete' ? 'text-emerald-600' : warning ? 'text-amber-600' : 'text-[var(--app-muted)]'
  const content = (
    <>
      <img src={iconSrc} alt="" aria-hidden="true" className="h-4 w-auto shrink-0" />
      <StatusIcon size={13} aria-hidden="true" className={statusClass} />
      <span className={missingEpisode ? 'text-red-700' : 'sr-only'}>{statusLabel}</span>
    </>
  )
  const className = 'inline-flex items-center gap-1.5 rounded-xl border px-2.5 py-1.5 text-[var(--app-text)] backdrop-blur ' +
    (missingEpisode ? 'border-red-500 bg-[var(--app-danger-soft)]' : 'border-[var(--app-border)] bg-[var(--app-panel)]/70') +
    (!missingEpisode && (href || onClick) ? ' hover:border-[var(--app-brand-border)] hover:text-[var(--app-brand-text)]' : '')
  const title = `${label}：${statusLabel}` + (missingEpisode ? '；已按本地季集号入库，可能尚未收录或分集编号不同，也可能尚未完成补全' : '')
  if (!href) {
    if (!onClick) return <span title={title} aria-label={title} className={className}>{content}</span>
    return <button type="button" onClick={onClick} title={`${title}，点击设置豆瓣 ID`} aria-label={`${title}，点击设置豆瓣 ID`} className={className}>{content}</button>
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={title}
      aria-label={`${title}，点击打开`}
      className={className}
    >
      {content}
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
