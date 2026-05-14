import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Play } from 'lucide-react'

import { mediaAPI } from '../api/library'
import type { Media } from '../types'

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

// Detail screen for a single media item: poster, summary and a Play CTA.
export function MediaDetailPage() {
  const { id = '' } = useParams()
  const [media, setMedia] = useState<Media | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    mediaAPI
      .get(id)
      .then(setMedia)
      .finally(() => setLoading(false))
  }, [id])

  if (loading) return <p className="text-slate-500">加载中…</p>
  if (!media) return <p className="text-slate-500">媒体不存在</p>

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-6 md:flex-row">
        <div className="aspect-[2/3] w-48 shrink-0 overflow-hidden rounded-xl bg-surface-800">
          {media.poster_url ? (
            <img src={media.poster_url} alt={media.title} className="h-full w-full object-cover" />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-slate-600">
              无海报
            </div>
          )}
        </div>
        <div className="flex-1 space-y-4">
          <h1 className="font-display text-3xl font-bold text-white">{media.title}</h1>
          {media.year > 0 && <p className="text-slate-400">{media.year}</p>}
          {media.overview && <p className="text-slate-300">{media.overview}</p>}

          <div className="flex flex-wrap gap-2 text-xs text-slate-400">
            {media.video_codec && <Badge>{media.video_codec.toUpperCase()}</Badge>}
            {media.width > 0 && (
              <Badge>
                {media.width}×{media.height}
              </Badge>
            )}
            <Badge>{fmtDuration(media.duration_sec)}</Badge>
            <Badge>{fmtSize(media.size_bytes)}</Badge>
            {media.container && <Badge>{media.container.toUpperCase()}</Badge>}
          </div>

          <Link to={`/play/${media.id}`} className="neon-button">
            <Play size={18} /> 播放
          </Link>
        </div>
      </div>
    </div>
  )
}

function Badge({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded border border-white/10 bg-white/5 px-2 py-0.5">{children}</span>
  )
}
