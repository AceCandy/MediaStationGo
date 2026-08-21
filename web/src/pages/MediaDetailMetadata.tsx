import { Calendar, Heart, Star } from 'lucide-react'
import { motion } from 'framer-motion'

import type { Media } from '../types'

type MediaDetailMetadataProps = {
  media: Media
  favourite: boolean
  onToggleFavourite: () => void
}

const rise = (delay: number) => ({
  initial: { opacity: 0, y: 14 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.5, delay, ease: [0.21, 0.47, 0.32, 0.98] as const },
})

export function MediaDetailMetadata({ media, favourite, onToggleFavourite }: MediaDetailMetadataProps) {
  const heading = media.title
  const seriesContext = media.series_title?.trim()

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
            className={
              'btn-outline shrink-0 gap-2 ' +
              (favourite
                ? '!border-red-200 !bg-red-50 !text-red-600 hover:!bg-red-100/50'
                : 'hover:border-red-200 hover:text-red-600 hover:bg-red-50/50')
            }
          >
            <Heart size={14} fill={favourite ? 'currentColor' : 'none'} aria-hidden="true" />
            <span>{favourite ? '取消收藏' : '加入收藏'}</span>
          </button>
        </motion.div>

        <motion.div {...rise(0.08)} className="flex flex-wrap items-center gap-2.5 text-xs font-bold tracking-wide">
          {media.rating > 0 && (
            <span className="badge-gold !px-3 !py-1.5 !text-xs shadow-glow-gold">
              <Star size={12} fill="currentColor" className="mr-1" />
              {media.rating.toFixed(1)}
            </span>
          )}
          {media.year > 0 && (
            <span className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-text)] backdrop-blur">
              <Calendar size={13} className="text-brand-500" />
              <span>{media.year} 年</span>
            </span>
          )}
          {media.width > 0 && (
            <span className="rounded-xl border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] px-3 py-1.5 uppercase text-[var(--app-brand-text)] backdrop-blur">
              {media.width} × {media.height}
            </span>
          )}
          <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-subtle)] backdrop-blur">
            {fmtDuration(media.duration_sec)}
          </span>
          <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 text-[var(--app-subtle)] backdrop-blur">
            {fmtSize(media.size_bytes)}
          </span>
          {media.container && (
            <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-3 py-1.5 font-mono text-[10px] uppercase text-[var(--app-subtle)] backdrop-blur">
              {media.container}
            </span>
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
      </motion.div>
    </>
  )
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
