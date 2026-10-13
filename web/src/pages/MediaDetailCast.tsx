import { useEffect, useState } from 'react'
import { motion } from 'framer-motion'
import { Clapperboard, UserRound } from 'lucide-react'

import { imageURL } from '../api/client'
import { mediaAPI } from '../api/library'
import type { MediaCredit } from '../types'

// MediaDetailCast 详情页演职员横滚：圆形头像 + 名字 + 角色，演员在前主创在后。
export function MediaDetailCast({ mediaId, scope }: { mediaId: string; scope?: 'series' }) {
  const [credits, setCredits] = useState<MediaCredit[] | null>(null)

  useEffect(() => {
    let cancelled = false
    setCredits(null)
    mediaAPI.listCredits(mediaId, scope)
      .then((items) => { if (!cancelled) setCredits(items) })
      .catch(() => { if (!cancelled) setCredits([]) })
    return () => { cancelled = true }
  }, [mediaId, scope])

  if (!credits || credits.length === 0) return null

  return <MediaCredits credits={credits} />
}

export function MediaCredits({ credits, onPersonClick, title = '演职员', total = credits.length, loading = false, error = '', progressive = false, onLoadMore }: {
  credits: MediaCredit[]; onPersonClick?: (credit: MediaCredit) => void; title?: string; total?: number
  loading?: boolean; error?: string; progressive?: boolean; onLoadMore?: () => void
}) {
  if (credits.length === 0 && !loading && !error) return null

  return (
    <section aria-label={title} className="min-w-0 space-y-4">
      <div className="flex items-center gap-2.5">
        <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] p-1.5 text-[var(--app-text)]">
          <Clapperboard size={15} />
        </span>
        <h2 className="font-display text-lg font-extrabold tracking-tight text-[var(--app-text)]">{title}</h2>
        <span className="rounded-full border border-[var(--app-border)] bg-[var(--app-panel-soft)] px-2.5 py-0.5 text-xs font-bold text-[var(--app-muted)]">
          {total}
        </span>
      </div>

      <div role={progressive ? 'region' : undefined} aria-label={progressive ? `${title}列表` : undefined} tabIndex={progressive ? 0 : undefined}
        className="-mx-1 overflow-x-auto px-1 pb-2 scrollbar-hide"
        onScroll={event => {
          const row = event.currentTarget
          if (row.scrollLeft > 0 && row.scrollWidth - row.clientWidth - row.scrollLeft < 240 && !loading && !error) onLoadMore?.()
        }}>
        <div className="flex gap-5">
          {credits.map((credit, index) => (
            <CastCard key={`${credit.person_id}:${credit.type}:${credit.role ?? ''}:${index}`} credit={credit} index={index} onClick={onPersonClick ? () => onPersonClick(credit) : undefined} />
          ))}
          {(loading || error || onLoadMore) && <div className="flex w-32 shrink-0 items-center justify-center text-center text-sm text-ink-50">
            {loading ? <span role="status">正在加载{title}…</span> : error ? <div role="alert"><p>{error}</p><button className="btn-outline mt-2" onClick={onLoadMore}>重试{title}</button></div> : <button className="btn-outline" onClick={onLoadMore}>加载更多{title}</button>}
          </div>}
        </div>
      </div>
    </section>
  )
}

function CastCard({ credit, index, onClick }: { credit: MediaCredit; index: number; onClick?: () => void }) {
  const [imageFailed, setImageFailed] = useState(false)
  const profileSrc = credit.profile_url && !imageFailed ? imageURL(credit.profile_url) : ''
  const crewLabel = creditTypeLabel(credit.type)
  const subtitle = credit.role?.trim() || crewLabel

  const Card = onClick ? motion.button : motion.div
  return (
    <Card
      onClick={onClick}
      aria-label={onClick ? `查看${credit.name}` : undefined}
      type={onClick ? 'button' : undefined}
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, delay: Math.min(index, 12) * 0.04, ease: [0.21, 0.47, 0.32, 0.98] }}
      className="group flex w-20 shrink-0 flex-col items-center gap-2 text-center"
      title={subtitle ? `${credit.name} · ${subtitle}` : credit.name}
    >
      <div className="relative h-18 w-18 overflow-hidden rounded-full border border-[var(--app-border)] shadow-sm transition-all duration-300 group-hover:-translate-y-1 group-hover:border-[var(--app-accent-border)] group-hover:shadow-glow-sm">
        {profileSrc ? (
          <img
            src={profileSrc}
            alt={credit.name}
            loading="lazy"
            decoding="async"
            onError={() => setImageFailed(true)}
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-[1.08]"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-[var(--app-muted)]" style={{ background: 'var(--app-poster-empty)' }}>
            <UserRound size={22} className="stroke-[1.5]" />
          </div>
        )}
      </div>
      <div className="w-full space-y-0.5">
        <p className="truncate text-xs font-bold text-[var(--app-text)] transition-colors group-hover:text-[var(--app-brand-text)]">
          {credit.name}
        </p>
        {subtitle && (
          <p className="truncate text-[10px] font-medium text-[var(--app-muted)]">{subtitle}</p>
        )}
      </div>
    </Card>
  )
}

function creditTypeLabel(creditType: string): string {
  switch (creditType) {
    case 'Director':
      return '导演'
    case 'Writer':
      return '编剧'
    case 'GuestStar':
      return '客串'
    default:
      return ''
  }
}
