import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Film, Heart, Layers, Star } from 'lucide-react'
import { imageURL } from '../api/client'
import type { Media } from '../types'
import { mediaDetailLink } from '../utils/groupSeries'

export const MediaCard = ({
  media, progress, count, rating, linkTo, linkState, onClick, favourite, onToggleFavourite, staggerIndex,
}: {
  media: Media
  progress?: number
  count?: number
  rating?: number
  linkTo?: string
  linkState?: { from: string }
  onClick?: () => void
  // 收藏角标：传入 onToggleFavourite 才渲染，favourite 控制红心跳常显
  favourite?: boolean
  onToggleFavourite?: () => void
  // 网格入场错落序号：delay = min(index, 14) * 0.035s
  staggerIndex?: number
}) => {
  const ref = useRef<HTMLDivElement>(null)
  const href = linkTo ?? mediaDetailLink(media)
  const [posterFit, setPosterFit] = useState<'cover' | 'contain'>('cover')
  const posterSrc = imageURL(media.poster_url, media.updated_at)
  const displayRating = rating ?? media.rating
  const versionCount = media.version_count ?? media.versions?.length ?? 0
  const entranceDelay = staggerIndex === undefined ? 0 : Math.min(staggerIndex, 14) * 0.035
  // 入场结束后清零 delay，避免拖慢后续 hover 弹簧
  const [motionDelay, setMotionDelay] = useState(entranceDelay)

  useEffect(() => {
    setMotionDelay(entranceDelay)
  }, [entranceDelay])

  useEffect(() => {
    setPosterFit('cover')
  }, [media.poster_url, media.updated_at])

  // 鼠标跟随高光：只写 CSS 变量，不触发 React 重渲染
  const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const el = ref.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    el.style.setProperty('--spot-x', `${event.clientX - rect.left}px`)
    el.style.setProperty('--spot-y', `${event.clientY - rect.top}px`)
  }

  const card = (
      <motion.div
        ref={ref}
        onPointerMove={handlePointerMove}
        initial={{ opacity: 0, y: 14 }}
        animate={{ opacity: 1, y: 0 }}
        whileHover={{ scale: 1.035, y: -6 }}
        transition={{ type: 'spring', stiffness: 300, damping: 26, delay: motionDelay }}
        onAnimationComplete={() => { if (motionDelay) setMotionDelay(0) }}
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

          {/* 鼠标跟随高光 */}
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 z-10 opacity-0 transition-opacity duration-300 group-hover:opacity-100"
            style={{
              background: 'radial-gradient(220px circle at var(--spot-x, 50%) var(--spot-y, 50%), rgba(167, 139, 250, 0.22), transparent 65%)',
            }}
          />

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
          <div className="absolute left-2.5 top-2.5 flex flex-col items-start gap-1">
            {displayRating > 0 && (
              <span className="inline-flex items-center gap-1 rounded-lg border border-white/15 bg-black/55 px-2 py-1 text-[10px] font-bold text-gold-400 backdrop-blur-md">
                <Star size={10} fill="currentColor" />
                <span>{displayRating.toFixed(1)}</span>
              </span>
            )}
            {media.douban_id && (
              <span title="豆瓣评分" className="inline-flex items-center gap-1 rounded-lg border border-white/15 bg-black/55 px-2 py-1 text-[10px] font-bold text-white backdrop-blur-md">
                <img src="/brand/douban.svg" alt="豆瓣" className="h-2.5 w-2.5" />
                <span>{media.douban_rating && media.douban_rating > 0 ? media.douban_rating.toFixed(1) : '-'}</span>
              </span>
            )}
          </div>

          {/* Hover Overlay */}
          <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/90 via-black/35 to-transparent p-4 opacity-0 transition-opacity duration-300 group-hover:opacity-100">
            <div className="translate-y-3 transition-transform duration-300 ease-smooth group-hover:translate-y-0">
              <p className="text-[11px] font-medium leading-relaxed text-white/75 line-clamp-2">
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
        <div className="space-y-1.5 border-t border-[var(--app-border)] p-4">
          <p className="truncate text-sm font-bold text-[var(--app-text)] transition-colors duration-200 group-hover:text-[var(--app-brand-text)]">
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

  // 收藏角标：右上角；与「N 集 / N 版本」角标同存时下移避开。置于 Link/button 之外以拦截点击。
  const hasCountBadge = (count !== undefined && count > 1) || (count === undefined && versionCount > 1)
  const favouriteButton = onToggleFavourite ? (
    <button
      type="button"
      aria-label={favourite ? '取消收藏' : '加入收藏'}
      aria-pressed={favourite}
      title={favourite ? '取消收藏' : '加入收藏'}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        onToggleFavourite()
      }}
      className={
        `absolute right-2 z-20 grid h-8 w-8 place-items-center rounded-full border backdrop-blur-md transition-all duration-200 ${hasCountBadge ? 'top-11' : 'top-2'} ` +
        (favourite
          ? 'border-red-400/50 bg-black/55 text-red-500 hover:bg-black/70'
          : 'border-white/25 bg-black/45 text-white opacity-0 hover:bg-black/60 hover:text-red-400 group-hover:opacity-100 focus-visible:opacity-100')
      }
    >
      <Heart size={14} fill={favourite ? 'currentColor' : 'none'} aria-hidden="true" />
    </button>
  ) : null

  if (onClick) {
    return (
      <div className="group relative block w-full">
        <button type="button" onClick={onClick} className="block w-full text-left">
          {card}
        </button>
        {favouriteButton}
      </div>
    )
  }

  if (favouriteButton) {
    return (
      <div className="group relative block">
        <Link to={href} state={linkState} className="block">
          {card}
        </Link>
        {favouriteButton}
      </div>
    )
  }

  return (
    <Link to={href} state={linkState} className="group block">
        {card}
    </Link>
  )
}
