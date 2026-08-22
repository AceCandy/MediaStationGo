import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { streamURL } from '../api/client'
import { playbackAPI } from '../api/playback'
import { subtitlesAPI, type SubtitleTrack } from '../api/subtitles'
import type { Media } from '../types'
import { mediaLibraryBackTarget } from './MediaDetailPageModel'
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
  const lastPositionRef = useRef(-1)
  const sessionIDRef = useRef('')

  const [media, setMedia] = useState<Media | null>(null)
  const [subs, setSubs] = useState<SubtitleTrack[]>([])
  const [playerError, setPlayerError] = useState('')

  const backTarget = useCallback(() => {
    const state = location.state as { from?: string } | null
    if (state?.from) return state.from
    if (media) {
      const target = mediaLibraryBackTarget(media)
      if (target) return target
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
    sessionIDRef.current = crypto.randomUUID()
    lastSentRef.current = 0
    lastPositionRef.current = -1
    video.src = streamURL(media.id)
    void video.play().catch(() => undefined)
  }, [media])

  // Persist periodically, then force the last distinct position on lifecycle boundaries.
  useEffect(() => {
    if (!media || !ref.current) return
    const video = ref.current
    const save = (force = false, keepalive = false) => {
      const now = Date.now()
      const durationMs = Math.floor((video.duration || 0) * 1000)
      if (durationMs <= 0) return
      const positionMs = Math.min(Math.floor(video.currentTime * 1000), durationMs)
      if (positionMs <= 0 || positionMs === lastPositionRef.current) return
      if (!force && now - lastSentRef.current < 10_000) return
      lastSentRef.current = now
      lastPositionRef.current = positionMs
      if (keepalive) {
        void playbackAPI.recordProgressKeepalive(media.id, sessionIDRef.current, positionMs, durationMs).catch(() => undefined)
        return
      }
      void playbackAPI.recordProgress(media.id, sessionIDRef.current, positionMs, durationMs)
        .catch(() => playbackAPI.recordProgress(media.id, sessionIDRef.current, positionMs, durationMs))
        .catch(() => undefined)
    }
    const periodicSave = () => save()
    const finalSave = () => save(true)
    const leaveSave = () => save(true, true)
    video.addEventListener('timeupdate', periodicSave)
    video.addEventListener('pause', finalSave)
    video.addEventListener('ended', finalSave)
    window.addEventListener('pagehide', leaveSave)
    return () => {
      video.removeEventListener('timeupdate', periodicSave)
      video.removeEventListener('pause', finalSave)
      video.removeEventListener('ended', finalSave)
      window.removeEventListener('pagehide', leaveSave)
      leaveSave()
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
