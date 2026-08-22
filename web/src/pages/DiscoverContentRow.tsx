import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { ChevronLeft, ChevronRight, Info } from 'lucide-react'

import type { DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'
import { discoverItemSource } from './discoverPageModel'

export function ContentRow({
  title,
  items,
  page = 1,
  canNext = false,
  onPageChange,
  onSelect,
}: {
  title: string
  items: DiscoverItem[]
  page?: number
  canNext?: boolean
  onPageChange?: (delta: number) => void
  onSelect: (item: DiscoverItem) => void
}) {
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="pl-1 font-display text-2xl font-semibold text-ink-600">{title}</h2>
        {onPageChange && (
          <div className="flex items-center gap-2">
            <button
              type="button"
              aria-label={`${title} 上一页`}
              disabled={page <= 1}
              onClick={() => onPageChange(-1)}
              className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-gray-200 bg-white text-ink-100 transition hover:border-primary-300 hover:text-brand-500 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <ChevronLeft size={16} />
            </button>
            <span className="min-w-10 text-center text-xs font-semibold text-sand-500">第 {page} 页</span>
            <button
              type="button"
              aria-label={`${title} 下一页`}
              disabled={!canNext}
              onClick={() => onPageChange(1)}
              className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-gray-200 bg-white text-ink-100 transition hover:border-primary-300 hover:text-brand-500 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <ChevronRight size={16} />
            </button>
          </div>
        )}
      </div>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
        {items.map((item, index) => (
          <DiscoverCard
            key={discoverKey(item, index)}
            item={item}
            onSelect={onSelect}
          />
        ))}
      </div>
    </section>
  )
}

export function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[0, 1, 2].map((section) => (
        <section key={section} className="space-y-4">
          <div className="skeleton h-8 w-48 rounded-xl" />
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
            {Array.from({ length: 8 }, (_, item) => (
              <div key={item} className="space-y-2">
                <div
                  className="skeleton aspect-[2/3] rounded-xl border border-[var(--app-border)]"
                  style={{ '--skeleton-delay': `${(section * 8 + item) * 0.12}s` } as CSSProperties}
                />
                <div className="skeleton h-3 w-3/4 rounded-md" />
                <div className="skeleton h-2.5 w-1/2 rounded-md" />
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function DiscoverCard({
  item,
  onSelect,
}: {
  item: DiscoverItem
  onSelect: (item: DiscoverItem) => void
}) {
  const source = discoverItemSource(item)
  const imageCandidates = useMemo(
    () =>
      [item.poster_url, item.backdrop_url]
        .map((value) => value?.trim())
        .filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index),
    [item.poster_url, item.backdrop_url],
  )
  const [imageIndex, setImageIndex] = useState(0)
  const [posterRetry, setPosterRetry] = useState(0)
  const [posterUnavailable, setPosterUnavailable] = useState(false)
  const posterVersion = posterRetry > 0 ? `r${posterRetry}` : undefined
  const activeImage = imageCandidates[imageIndex] ?? ''
  const posterSrc = useMemo(
    () =>
      imageURL(activeImage, posterVersion, {
        refreshCache: posterRetry > 0,
        retryFailed: true,
      }),
    [activeImage, posterRetry, posterVersion],
  )

  useEffect(() => {
    setImageIndex(0)
    setPosterRetry(0)
    setPosterUnavailable(false)
  }, [item.poster_url, item.backdrop_url])

  useEffect(() => {
    if (!posterUnavailable) return
    if (imageIndex + 1 < imageCandidates.length) {
      const timer = window.setTimeout(() => {
        setImageIndex((current) => Math.min(current + 1, imageCandidates.length - 1))
        setPosterRetry(0)
        setPosterUnavailable(false)
      }, 150)
      return () => window.clearTimeout(timer)
    }
    if (posterRetry >= 3) return
    const timer = window.setTimeout(() => {
      setPosterRetry((current) => current + 1)
      setPosterUnavailable(false)
    }, 1200 * (posterRetry + 1))
    return () => window.clearTimeout(timer)
  }, [imageCandidates.length, imageIndex, posterRetry, posterUnavailable])

  const markPosterUnavailable = () => setPosterUnavailable(true)

  if (!posterSrc || posterUnavailable) return null

  return (
    <button
      type="button"
      onClick={() => onSelect(item)}
      className="group relative overflow-hidden rounded-xl border border-gray-200 bg-gray-50 text-left transition-all duration-300 hover:-translate-y-1 hover:border-primary-500/30 hover:shadow-xl focus:outline-none focus:ring-2 focus:ring-primary-400/40"
    >
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-surface-900">
        {posterSrc && (
          <img
            src={posterSrc}
            alt={item.title}
            loading="lazy"
            decoding="async"
            referrerPolicy="no-referrer"
            onError={markPosterUnavailable}
            onLoad={(event) => {
              const img = event.currentTarget
              if (img.naturalWidth <= 1 && img.naturalHeight <= 1) {
                markPosterUnavailable()
              }
            }}
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
          />
        )}
        <div className="absolute left-1.5 top-1.5 rounded-xl border border-white/20 bg-black/65 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-white backdrop-blur-sm">
          {source}
        </div>
        {(item.rating ?? 0) > 0 && (
          <div className="absolute right-1.5 top-1.5 rounded-xl border border-yellow-400/30 bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-yellow-400 backdrop-blur-sm">
            ★ {(item.rating ?? 0).toFixed(1)}
          </div>
        )}
      </div>
      <div className="space-y-0.5 px-2.5 py-2">
        <p className="truncate text-xs font-medium text-ink-600 transition-colors group-hover:text-brand-500">
          {item.title}
        </p>
        <p className="text-[11px] text-sand-500">
          {[item.media_type, item.year && item.year > 0 ? item.year : ''].filter(Boolean).join(' · ') || '推荐'}
        </p>
        <p className="flex items-center gap-1 pt-1 text-[10px] font-semibold text-brand-500">
          <Info size={10} />
          查看详情
        </p>
      </div>
    </button>
  )
}

function discoverKey(item: DiscoverItem, index: number): string {
  return `${item.source || 'source'}:${item.tmdb_id || item.douban_id || item.bangumi_id || item.title}:${index}`
}
