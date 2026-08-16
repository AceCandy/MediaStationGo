import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Film, Play, Layers, Star } from 'lucide-react'
import { imageURL } from '../api/client'
import type { Media } from '../types'
import { mediaDetailLink } from '../utils/groupSeries'

export const MediaCard = ({
  media, progress, count, rating, linkTo, onClick, actions,
}: {
  media: Media
  progress?: number
  count?: number
  rating?: number
  linkTo?: string
  onClick?: () => void
  actions?: ReactNode
}) => {
  const ref = useRef<HTMLDivElement>(null)
  const href = linkTo ?? mediaDetailLink(media)
  const [posterFit, setPosterFit] = useState<'cover' | 'contain'>('cover')
  const posterSrc = imageURL(media.poster_url, media.updated_at)
  const displayRating = rating ?? media.rating
  const versionCount = media.versions?.length ?? 0

  useEffect(() => {
    setPosterFit('cover')
  }, [media.poster_url, media.updated_at])

  const card = (
      <motion.div
        ref={ref}
        initial={{ opacity: 0, y: 14 }}
        animate={{ opacity: 1, y: 0 }}
        whileHover={{ scale: 1.035, y: -6 }}
        transition={{ type: 'spring', stiffness: 300, damping: 26 }}
        className="relative overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] shadow-poster transition-[border-color,box-shadow] duration-300 hover:border-[var(--app-accent-border)] hover:shadow-poster-hover"
      >
        {/* Poster Wrapper */}
        <div className="relative aspect-[2/3] w-full overflow-hidden bg-[var(--app-panel-soft)]">
          {media.poster_url ? (
            <>
              {posterFit === 'contain' && (
                <img
                  src={posterSrc}
                  alt=""
                  aria-hidden="true"
                  loading="lazy"
                  className="absolute inset-0 h-full w-full scale-110 object-cover object-center opacity-25 blur-xl"
                  referrerPolicy="no-referrer"
                />
              )}
              <img
                src={posterSrc}
                alt={media.title}
                loading="lazy"
                decoding="async"
                onLoad={(event) => {
                  const img = event.currentTarget
                  setPosterFit(img.naturalWidth > img.naturalHeight ? 'contain' : 'cover')
                }}
                className={
                  'relative block h-full w-full object-center transition-transform duration-700 ease-smooth group-hover:scale-[1.06] ' +
                  (posterFit === 'contain' ? 'object-contain p-1.5' : 'object-cover')
                }
                referrerPolicy="no-referrer"
              />
            </>
          ) : (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-[var(--app-muted)]" style={{ background: 'var(--app-poster-empty)' }}>
              <Film size={28} className="stroke-[1.5]" />
              <span className="text-[10px] uppercase tracking-wider font-bold">No Poster</span>
            </div>
          )}

          {/* Episode count badge */}
          {count !== undefined && count > 1 && (
            <span className="absolute right-2.5 top-2.5 inline-flex items-center gap-1 rounded-lg border border-white/15 bg-black/55 px-2 py-1 text-[10px] font-bold text-white backdrop-blur-md">
              <Layers size={10} className="text-brand-300" />
              <span>{count} 集</span>
            </span>
          )}

          {count === undefined && versionCount > 1 && (
            <span className="absolute right-2.5 top-2.5 inline-flex items-center gap-1 rounded-lg border border-white/15 bg-black/55 px-2 py-1 text-[10px] font-bold text-white backdrop-blur-md">
              <Layers size={10} className="text-brand-300" />
              <span>{versionCount} 版本</span>
            </span>
          )}

          {/* Rating Badge — 星光金 */}
          {displayRating > 0 && (
            <span className="absolute left-2.5 top-2.5 inline-flex items-center gap-1 rounded-lg border border-white/15 bg-black/55 px-2 py-1 text-[10px] font-bold text-gold-400 backdrop-blur-md">
              <Star size={10} fill="currentColor" />
              <span>{displayRating.toFixed(1)}</span>
            </span>
          )}

          {/* Hover Overlay */}
          <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/90 via-black/35 to-transparent p-4 opacity-0 transition-opacity duration-300 group-hover:opacity-100">
            <div className="translate-y-3 space-y-2.5 transition-transform duration-300 ease-smooth group-hover:translate-y-0">
              <span className="inline-flex items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-bold text-white shadow-glow-sm"
                style={{ background: 'linear-gradient(135deg, #8b5cf6 0%, #7c3aed 60%, #6d28d9 100%)' }}>
                <Play size={10} fill="currentColor" />
                <span>立即观影</span>
              </span>
              <p className="text-[10px] font-medium leading-relaxed text-white/75 line-clamp-2">
                {media.overview || '暂无简介内容'}
              </p>
            </div>
          </div>

          {/* Progress Bar overlay */}
          {progress !== undefined && progress > 0 && progress < 1 && (
            <div className="absolute inset-x-0 bottom-0 h-1 bg-black/40">
              <div
                className="h-full rounded-r-full transition-all duration-300"
                style={{
                  width: `${Math.round(progress * 100)}%`,
                  background: 'linear-gradient(90deg, #8b5cf6, #d946ef)',
                  boxShadow: '0 0 8px rgba(139,92,246,0.6)',
                }}
              />
            </div>
          )}
        </div>

        {/* Media Metadata Info */}
        <div className="space-y-1 border-t border-[var(--app-border)] p-3.5">
          <p className="truncate text-[13px] font-bold text-[var(--app-text)] transition-colors duration-200 group-hover:text-[var(--app-brand-text)]">
            {media.title}
          </p>
          <div className="flex items-center justify-between text-[10px] font-bold uppercase tracking-wider text-[var(--app-muted)]">
            <span>{media.year > 0 ? media.year : '未知年份'}</span>
            {media.video_codec && (
              <span className="rounded-md border border-[var(--app-border)] bg-[var(--app-panel-soft)] px-1.5 py-0.5 text-[var(--app-subtle)]">
                {media.video_codec}
              </span>
            )}
          </div>
        </div>
      </motion.div>
  )

  if (onClick) {
    return (
      <div className="group relative block w-full">
        <button type="button" onClick={onClick} className="block w-full text-left">
          {card}
        </button>
        {actions && (
          <div className="absolute right-2 top-2 z-20 flex flex-wrap justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            {actions}
          </div>
        )}
      </div>
    )
  }

  if (actions) {
    return (
      <div className="group relative block">
        <Link to={href} className="block">
          {card}
        </Link>
        <div className="absolute right-2 top-2 z-20 flex flex-wrap justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          {actions}
        </div>
      </div>
    )
  }

  return (
    <Link to={href} className="group block">
        {card}
    </Link>
  )
}
