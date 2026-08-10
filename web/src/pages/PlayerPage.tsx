import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { streamURL } from '../api/client'
import { playbackAPI } from '../api/playback'
import { subtitlesAPI, type SubtitleTrack } from '../api/subtitles'
import type { Media } from '../types'
import { getSeriesKey, isEpisodeLike } from '../utils/groupSeries'
import { PlayerTopBar } from './PlayerTopBar'
import { PlayerVideoStage } from './PlayerVideoStage'

// Fullscreen, dark-themed video page.
//
// External subtitles next to the source file are auto-discovered and
// attached as <track> elements.
export function PlayerPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()

  const ref = useRef<HTMLVideoElement>(null)
  const lastSentRef = useRef(0)

  const [media, setMedia] = useState<Media | null>(null)
  const [subs, setSubs] = useState<SubtitleTrack[]>([])
  const [playerError, setPlayerError] = useState('')

  const backTarget = useCallback(() => {
    const state = location.state as { from?: string } | null
    if (state?.from) return state.from
    if (media && isEpisodeLike(media) && media.library_id) {
      return `/library/${encodeURIComponent(media.display_library_id || media.library_id)}?series=${encodeURIComponent(getSeriesKey(media))}`
    }
    const target = media?.id || id
    return target ? `/media/${target}` : '/'
  }, [id, location.state, media])

  const goBack = useCallback(() => {
    navigate(backTarget(), { replace: true })
  }, [backTarget, navigate])

  // Load metadata and subtitle sidecars.
  useEffect(() => {
    if (!id) return
    mediaAPI.get(id).then((m) => {
      setMedia(m)
      setPlayerError('')
    })
    subtitlesAPI
      .list(id)
      .then((tracks) => setSubs(tracks ?? []))
      .catch(() => setSubs([]))
  }, [id])

  // Serve the original source unchanged. Codec support belongs to the client.
  useEffect(() => {
    if (!media || !ref.current) return
    const video = ref.current
    video.src = streamURL(media.id)
    void video.play().catch(() => undefined)
  }, [media])

  // Persist resume position every 10 seconds while playing.
  useEffect(() => {
    if (!media || !ref.current) return
    const video = ref.current
    const handler = () => {
      const now = Date.now()
      if (now - lastSentRef.current < 10_000) return
      lastSentRef.current = now
      const positionMs = Math.floor(video.currentTime * 1000)
      const durationMs = Math.floor((video.duration || 0) * 1000)
      if (positionMs > 0) {
        playbackAPI.recordProgress(media.id, positionMs, durationMs).catch(() => undefined)
      }
    }
    video.addEventListener('timeupdate', handler)
    video.addEventListener('pause', handler)
    return () => {
      video.removeEventListener('timeupdate', handler)
      video.removeEventListener('pause', handler)
    }
  }, [media])

  // ESC = back.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') goBack()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [goBack])

  const handleVideoError = useCallback(() => {
    if (media?.strm_url?.trim()) {
      setPlayerError('STRM 直连播放失败。请检查源地址是否可访问以及当前客户端是否支持该媒体格式。')
      toast.error('STRM 直连播放失败')
    } else {
      setPlayerError('当前客户端无法直接播放该媒体格式。')
      toast.error('直接播放失败')
    }
  }, [media])

  return (
    <div className="relative -m-6 flex min-h-screen flex-col overflow-hidden bg-black md:-m-8">
      <PlayerTopBar onBack={goBack} />
      <PlayerVideoStage
        media={media}
        playerError={playerError}
        subs={subs}
        videoRef={ref}
        onVideoError={handleVideoError}
      />
    </div>
  )
}
