import { Calendar, Heart } from 'lucide-react'

import type { Media } from '../types'

type MediaDetailMetadataProps = {
  media: Media
  favourite: boolean
  onToggleFavourite: () => void
}

export function MediaDetailMetadata({ media, favourite, onToggleFavourite }: MediaDetailMetadataProps) {
  const heading = media.title
  const seriesContext = media.series_title?.trim()

  return (
    <>
      <div className="space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="min-w-0 break-words text-pretty font-display text-3xl sm:text-4xl font-extrabold tracking-tight text-gray-900 leading-tight">
            {heading}
          </h1>
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
        </div>
        {seriesContext && (
          <p className="text-sm font-semibold text-gray-500">
            {seriesContext}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-2.5 text-xs text-gray-500 font-bold tracking-wide uppercase">
          {media.year > 0 && (
            <span className="inline-flex items-center gap-1 bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
              <Calendar size={13} className="text-brand-500" />
              <span>{media.year} 年</span>
            </span>
          )}
          {media.width > 0 && (
            <span className="inline-flex items-center gap-1 bg-brand-50 text-brand-700 border border-brand-100/50 px-2.5 py-1 rounded-xl">
              <span>{media.width} × {media.height}</span>
            </span>
          )}
          <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
            {fmtSize(media.size_bytes)}
          </span>
          <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700">
            {fmtDuration(media.duration_sec)}
          </span>
          {media.container && (
            <span className="bg-gray-100 border border-gray-200/50 px-2.5 py-1 rounded-xl text-gray-700 font-mono">
              {media.container}
            </span>
          )}
        </div>
      </div>

      {media.overview && (
        <div className="rounded-2xl bg-gray-50/50 border border-gray-100 p-5 space-y-2">
          <h3 className="text-xs font-bold uppercase tracking-widest text-brand-500">剧情简介</h3>
          <p className="text-sm text-gray-600 leading-relaxed font-semibold">
            {media.overview}
          </p>
        </div>
      )}

      <div className="space-y-4">
        <MetadataTags label="类型流派" values={parseCSV(media.genres)} primary />
        <div className="grid gap-4 sm:grid-cols-2">
          <MetadataTags label="国家/地区" values={localizedCSV(media.countries, 'region')} />
          <MetadataTags label="语言" values={localizedCSV(media.languages, 'language')} />
        </div>
      </div>
    </>
  )
}

function MetadataTags({ label, values, primary = false }: { label: string; values: string[]; primary?: boolean }) {
  if (values.length === 0) return null
  const tagClass = primary
    ? 'rounded-full bg-brand-50 text-brand-700 border border-brand-100/30 px-3 py-1 text-2xs font-bold uppercase tracking-wider'
    : 'rounded-xl bg-gray-100 text-gray-600 border border-gray-200/40 px-2.5 py-1 text-2xs font-semibold'
  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="w-16 whitespace-nowrap text-xs font-bold uppercase tracking-wider text-gray-500">{label}</span>
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
