import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

import { mediaAPI } from '../api/library'
import { streamURL } from '../api/client'
import type { Media } from '../types'

// Fullscreen, dark-themed video page. We currently use direct play via
// /api/stream/:id — the future task is wiring HLS.js for transcoded streams.
export function PlayerPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const ref = useRef<HTMLVideoElement>(null)
  const [media, setMedia] = useState<Media | null>(null)

  useEffect(() => {
    if (!id) return
    mediaAPI.get(id).then(setMedia)
  }, [id])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') navigate(-1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])

  return (
    <div className="-m-6 flex min-h-screen flex-col bg-black md:-m-8">
      <button
        onClick={() => navigate(-1)}
        className="absolute left-4 top-4 z-10 flex items-center gap-2 rounded-full bg-black/60 px-3 py-1.5 text-sm text-white backdrop-blur transition hover:bg-black/80"
      >
        <ArrowLeft size={16} /> 返回
      </button>
      <div className="flex flex-1 items-center justify-center">
        {media ? (
          <video
            ref={ref}
            src={streamURL(media.id)}
            controls
            autoPlay
            className="max-h-screen w-full max-w-[1600px] bg-black"
          />
        ) : (
          <p className="text-slate-500">加载中…</p>
        )}
      </div>
    </div>
  )
}
