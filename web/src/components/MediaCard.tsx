import { Link } from 'react-router-dom'
import { Film } from 'lucide-react'

import type { Media } from '../types'

// Compact poster tile used by every listing surface (home / library / search).
export function MediaCard({ media }: { media: Media }) {
  return (
    <Link
      to={`/media/${media.id}`}
      className="group block overflow-hidden rounded-xl border border-white/5 bg-surface-800/60 transition hover:border-primary-400/50 hover:shadow-lg hover:shadow-primary-400/20"
    >
      <div className="aspect-[2/3] w-full bg-surface-900">
        {media.poster_url ? (
          <img
            src={media.poster_url}
            alt={media.title}
            loading="lazy"
            className="h-full w-full object-cover transition group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-slate-600">
            <Film size={48} />
          </div>
        )}
      </div>
      <div className="px-3 py-2">
        <p className="truncate text-sm font-medium text-white">{media.title}</p>
        {media.year > 0 && <p className="text-xs text-slate-400">{media.year}</p>}
      </div>
    </Link>
  )
}
