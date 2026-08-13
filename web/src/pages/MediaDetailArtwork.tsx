import { motion } from 'framer-motion'
import { FileText, Play } from 'lucide-react'
import { Link } from 'react-router-dom'

import { imageURL } from '../api/client'
import type { Media } from '../types'

type MediaDetailArtworkProps = {
  media: Media
}

export function MediaDetailBackdrop({ media }: MediaDetailArtworkProps) {
  return (
    <div className="absolute inset-0 h-[520px] z-0 overflow-hidden">
      {media.backdrop_url || media.poster_url ? (
        <img
          src={imageURL(media.backdrop_url || media.poster_url || '', media.updated_at)}
          alt=""
          className="h-full w-full scale-105 object-cover object-[center_18%]"
          style={{ opacity: 'var(--app-backdrop-img-opacity, 0.35)' }}
          referrerPolicy="no-referrer"
        />
      ) : (
        <div className="theme-hero-bg h-full w-full" />
      )}
      <div className="absolute inset-0" style={{ background: 'var(--app-backdrop-overlay)' }} />
    </div>
  )
}

export function MediaDetailPoster({ media }: MediaDetailArtworkProps) {
  return (
    <div className="w-56 shrink-0 mx-auto md:mx-0">
      <motion.div
        whileHover={{ scale: 1.02 }}
        className="aspect-[2/3] w-full rounded-2xl overflow-hidden bg-gray-50 border border-gray-200 shadow-md relative group"
      >
        {media.poster_url ? (
          <img
            src={imageURL(media.poster_url, media.updated_at)}
            alt={media.title}
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="flex h-full w-full flex-col items-center justify-center gap-2 text-gray-500 bg-gray-50">
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
      </motion.div>
    </div>
  )
}
