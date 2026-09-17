import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'

import { discoverAPI, discoverIdentityKey, discoverTMDbIdentity, type DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'
import { discoverItemSource } from './discoverPageModel'

export function ContentRow({
  items,
  canNext = false,
  loading = false,
  onLoadMore,
  onSelect,
}: {
  items: DiscoverItem[]
  canNext?: boolean
  loading?: boolean
  onLoadMore?: () => void
  onSelect: (item: DiscoverItem) => void
}) {
  const sentinelRef = useRef<HTMLDivElement>(null)
  const [libraryItems, setLibraryItems] = useState<Set<string>>(new Set())
  const [libraryError, setLibraryError] = useState(false)
  const [libraryRevision, setLibraryRevision] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    const identities = Array.from(new Map(items.flatMap((item) => {
      const id = discoverTMDbIdentity(item)
      return id ? [[discoverIdentityKey(id), id] as const] : []
    })).values())
    setLibraryItems(new Set())
    setLibraryError(false)
    void (async () => {
      const found = new Set<string>()
      for (let start = 0; start < identities.length; start += 100) {
        const result = await discoverAPI.libraryStatus(identities.slice(start, start + 100), controller.signal)
        if (controller.signal.aborted) return
        result.forEach((id) => found.add(discoverIdentityKey(id)))
      }
      if (!controller.signal.aborted) setLibraryItems(found)
    })().catch(() => { if (!controller.signal.aborted) setLibraryError(true) })
    return () => controller.abort()
  }, [items, libraryRevision])
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel || !canNext || loading || !onLoadMore) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) onLoadMore()
    }, { rootMargin: '320px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [canNext, loading, onLoadMore])

  return (
    <section className="space-y-4">
      {libraryError && <p role="alert" className="text-sm text-[var(--app-muted)]">入库状态暂时不可用 <button className="btn-ghost" onClick={() => setLibraryRevision((v) => v + 1)}>重试入库状态</button></p>}
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5">
        {items.map((item, index) => (
          <DiscoverCard
            key={discoverKey(item, index)}
            item={item}
            inLibrary={libraryItems.has(`${item.media_type}:${item.tmdb_id}`) && discoverTMDbIdentity(item) !== null}
            onSelect={onSelect}
          />
        ))}
      </div>
      <div ref={sentinelRef} className="h-px" aria-hidden="true" />
      {loading && <p role="status" className="py-3 text-center text-sm text-ink-50">加载更多…</p>}
    </section>
  )
}

export function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[0, 1, 2].map((section) => (
        <section key={section} className="space-y-4">
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5">
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
  inLibrary,
  onSelect,
}: {
  item: DiscoverItem
  inLibrary: boolean
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
      aria-label={`查看${item.title}`}
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
        {inLibrary && <span className="absolute bottom-2 left-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)] px-2 py-1 text-[10px] font-semibold text-[var(--app-text)] shadow-sm">✅ 已入库</span>}
      </div>
      <div className="space-y-0.5 px-2.5 py-2">
        <p className="truncate text-xs font-medium text-ink-600 transition-colors group-hover:text-brand-500">
          {item.title}
        </p>
        <p className="text-[11px] text-sand-500">
          {[item.media_type, item.year && item.year > 0 ? item.year : ''].filter(Boolean).join(' · ') || '推荐'}
        </p>
      </div>
    </button>
  )
}

function discoverKey(item: DiscoverItem, index: number): string {
  return `${item.source || 'source'}:${item.tmdb_id || item.douban_id || item.bangumi_id || item.title}:${index}`
}
