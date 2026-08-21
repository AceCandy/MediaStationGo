import { motion } from 'framer-motion'
import { FileText, Play } from 'lucide-react'
import { Link } from 'react-router-dom'

import { imageURL } from '../api/client'
import { useDominantColor } from '../hooks/useDominantColor'
import type { Media } from '../types'

type MediaDetailArtworkProps = {
  media: Media
}

// MediaDetailBackdrop 影院式背景：backdrop 大图 + 按主色染色的环境光晕 + 可读性遮罩。
export function MediaDetailBackdrop({ media }: MediaDetailArtworkProps) {
  const backdropSrc = media.backdrop_url || media.poster_url
    ? imageURL(media.backdrop_url || media.poster_url || '', media.updated_at)
    : ''
  const ambient = useDominantColor(backdropSrc || null)

  return (
    <div className="absolute inset-0 h-[560px] z-0 overflow-hidden">
      {backdropSrc ? (
        <img
          src={backdropSrc}
          alt=""
          className="h-full w-full scale-105 object-cover object-[center_18%]"
          style={{ opacity: 'var(--app-backdrop-img-opacity, 0.35)' }}
          referrerPolicy="no-referrer"
        />
      ) : (
        <div className="theme-hero-bg h-full w-full" />
      )}
      {/* 环境光晕：主色染出两团缓慢漂移的光，叠加品牌紫保底 */}
      <motion.div
        aria-hidden="true"
        animate={{ opacity: ambient ? 1 : 0 }}
        transition={{ duration: 1.2, ease: 'easeOut' }}
        className="absolute inset-0 pointer-events-none"
      >
        {ambient && (
          <>
            <div
              className="absolute -top-24 right-[8%] h-80 w-[36rem] rounded-full blur-3xl animate-glow-drift"
              style={{ background: `radial-gradient(closest-side, rgba(${ambient}, 0.34), transparent)` }}
            />
            <div
              className="absolute top-40 -left-24 h-72 w-[30rem] rounded-full blur-3xl"
              style={{ background: `radial-gradient(closest-side, rgba(${ambient}, 0.2), transparent)` }}
            />
          </>
        )}
      </motion.div>
      <div className="absolute inset-0" style={{ background: 'var(--app-backdrop-overlay)' }} />
    </div>
  )
}

export function MediaDetailPoster({ media }: MediaDetailArtworkProps) {
  const posterSrc = media.poster_url ? imageURL(media.poster_url, media.updated_at) : ''
  const ambient = useDominantColor(posterSrc || null)

  return (
    <div className="w-full shrink-0">
      <motion.div
        initial={{ opacity: 0, y: 24, rotate: 1.5 }}
        animate={{ opacity: 1, y: 0, rotate: 0 }}
        whileHover={{ y: -6 }}
        transition={{ duration: 0.55, type: 'spring', stiffness: 140, damping: 18 }}
        className="relative"
      >
        {/* 海报主色投影：让海报像浮在环境光里 */}
        <div
          aria-hidden="true"
          className="absolute -inset-5 rounded-[2rem] blur-2xl transition-opacity duration-700"
          style={{
            background: ambient ? `rgba(${ambient}, 0.4)` : 'rgba(139, 92, 246, 0.25)',
            opacity: ambient ? 1 : 0.6,
          }}
        />
        <div className="group relative aspect-[2/3] w-full overflow-hidden rounded-2xl border border-white/15 shadow-[0_32px_80px_rgba(0,0,0,0.45)] bg-gray-50">
          {posterSrc ? (
            <img
              src={posterSrc}
              alt={media.title}
              className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
              referrerPolicy="no-referrer"
            />
          ) : (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-gray-500" style={{ background: 'var(--app-poster-empty)' }}>
              <FileText size={40} className="stroke-[1]" />
              <span className="text-xs uppercase tracking-wider font-bold">无海报</span>
            </div>
          )}

          <Link
            to={`/play/${media.id}`}
            className="absolute inset-0 flex items-center justify-center bg-black/45 opacity-0 backdrop-blur-[2px] transition-opacity duration-300 group-hover:opacity-100"
          >
            <div className="flex h-16 w-16 scale-90 items-center justify-center rounded-full text-white shadow-glow transition-transform duration-300 group-hover:scale-100"
              style={{ background: 'linear-gradient(135deg, #8b5cf6 0%, #7c3aed 60%, #6d28d9 100%)' }}>
              <Play size={26} fill="currentColor" />
            </div>
          </Link>
        </div>
      </motion.div>
    </div>
  )
}
