import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import type { NavigateFunction } from 'react-router-dom'
import toast from 'react-hot-toast'

import { api } from '../api/client'
import { mediaAPI } from '../api/library'
import { playbackAPI } from '../api/playback'
import { confirmAction } from '../components/confirmAction'
import type { Media } from '../types'
import { mediaLibraryBackTarget } from './MediaDetailPageModel'

interface MediaDetailPageStateParams {
  id: string
  navigate: NavigateFunction
}

interface MediaDetailRefreshParams {
  id: string
  setMedia: Dispatch<SetStateAction<Media | null>>
  setFavourite: Dispatch<SetStateAction<boolean>>
  setLoading: Dispatch<SetStateAction<boolean>>
}

interface MediaDetailActionsParams {
  media: Media | null
  navigate: NavigateFunction
  refresh: MediaDetailRefresh
  setFavourite: Dispatch<SetStateAction<boolean>>
}

type MediaDetailRefresh = () => Promise<Media | null>

export function useMediaDetailPageState({ id, navigate }: MediaDetailPageStateParams) {
  const [media, setMedia] = useState<Media | null>(null)
  const [versions, setVersions] = useState<Media[]>([])
  const [selectedVersionID, setSelectedVersionID] = useState(id)
  const [selectedMedia, setSelectedMedia] = useState<Media | null>(null)
  const [favourite, setFavourite] = useState(false)
  const [loading, setLoading] = useState(true)
  const [probing, setProbing] = useState(false)
  const [probeError, setProbeError] = useState('')
  const [selectedMediaLoading, setSelectedMediaLoading] = useState(false)
  const [selectedMediaProbing, setSelectedMediaProbing] = useState(false)
  const [selectedMediaError, setSelectedMediaError] = useState('')
  const [manualScrapeOpen, setManualScrapeOpen] = useState(false)
  const [metadataEditOpen, setMetadataEditOpen] = useState(false)
  const [organizeOpen, setOrganizeOpen] = useState(false)

  const refresh = useMediaDetailRefresh({ id, setMedia, setFavourite, setLoading })
  const actions = useMediaDetailActions({
    media,
    navigate,
    refresh,
    setFavourite,
  })

  useEffect(() => {
    let cancelled = false
    setVersions([])
    setSelectedVersionID(id)
    setSelectedMedia(null)
    setProbing(false)
    setProbeError('')
    refresh()
      .then(async (nextMedia) => {
        if (cancelled || !nextMedia) return
        setVersions([nextMedia])
        setSelectedVersionID(nextMedia.id)
        void mediaAPI.listVersions(id).catch(() => [nextMedia]).then((items) => {
          if (cancelled || items.length === 0) return
          setVersions(items)
          setSelectedVersionID(items[0].id)
        })
        if ((nextMedia.tracks?.length ?? 0) > 0) return
        setProbing(true)
        setProbeError('')
        try {
          const probed = await mediaAPI.ensureProbe(nextMedia.id)
          if (!cancelled) {
            setMedia(probed)
            setVersions((items) => items.map((item) => item.id === probed.id ? probed : item))
          }
        } catch {
          if (!cancelled) setProbeError('媒体信息探测失败，请检查 ffprobe 或媒体源是否可用')
        } finally {
          if (!cancelled) setProbing(false)
        }
      })
      .catch(() => undefined)
    return () => { cancelled = true }
  }, [id, refresh])

  useEffect(() => {
    const currentMediaID = media?.id
    if (!currentMediaID || !selectedVersionID || selectedVersionID === currentMediaID) {
      setSelectedMedia(null)
      setSelectedMediaLoading(false)
      setSelectedMediaProbing(false)
      setSelectedMediaError('')
      return
    }
    let cancelled = false
    setSelectedMedia(null)
    setSelectedMediaLoading(true)
    setSelectedMediaProbing(false)
    setSelectedMediaError('')
    mediaAPI.get(selectedVersionID)
      .then(async (nextMedia) => {
        if (cancelled) return
        setSelectedMedia(nextMedia)
        setSelectedMediaLoading(false)
        if ((nextMedia.tracks?.length ?? 0) > 0) return
        setSelectedMediaProbing(true)
        try {
          const probed = await mediaAPI.ensureProbe(nextMedia.id)
          if (!cancelled) {
            setSelectedMedia(probed)
            setVersions((items) => items.map((item) => item.id === probed.id ? probed : item))
          }
        } catch {
          if (!cancelled) setSelectedMediaError('媒体信息探测失败，请检查 ffprobe 或媒体源是否可用')
        } finally {
          if (!cancelled) setSelectedMediaProbing(false)
        }
      })
      .catch(() => {
        if (!cancelled) setSelectedMediaError('媒体版本加载失败')
      })
      .finally(() => {
        if (!cancelled) setSelectedMediaLoading(false)
      })
    return () => { cancelled = true }
  }, [id, media?.id, selectedVersionID])

  const handleMetadataSaved = useCallback(async (next: Media) => {
    setMedia(next)
    await refresh()
  }, [refresh])

  const showingCurrentMedia = !media || selectedVersionID === media.id

  return {
    media,
    versions: versions.length > 0 ? versions : media ? [media] : [],
    selectedVersionID,
    displayMedia: showingCurrentMedia ? media : selectedMedia,
    mediaInfoLoading: !showingCurrentMedia && selectedMediaLoading,
    favourite,
    loading,
    probing: showingCurrentMedia ? probing : selectedMediaProbing,
    probeError: showingCurrentMedia ? probeError : selectedMediaError,
    manualScrapeOpen,
    metadataEditOpen,
    organizeOpen,
    refresh,
    handleMetadataSaved,
    setManualScrapeOpen,
    setMetadataEditOpen,
    setOrganizeOpen,
    selectVersion: setSelectedVersionID,
    ...actions,
  }
}

function useMediaDetailRefresh({
  id,
  setMedia,
  setFavourite,
  setLoading,
}: MediaDetailRefreshParams): MediaDetailRefresh {
  const generation = useRef(0)
  useEffect(() => {
    generation.current += 1
    return () => { generation.current += 1 }
  }, [id])
  return useCallback(async () => {
    if (!id) return null
    const requestGeneration = ++generation.current
    setLoading(true)
    try {
      const nextMedia = await mediaAPI.get(id)
      if (requestGeneration !== generation.current) return null
      setMedia(nextMedia)
      setLoading(false)
      void playbackAPI.favouriteStatus(nextMedia.id)
        .then((state) => {
          if (requestGeneration === generation.current) setFavourite(state)
        })
        .catch(() => {
          if (requestGeneration === generation.current) setFavourite(false)
        })
      return nextMedia
    } finally {
      if (requestGeneration === generation.current) setLoading(false)
    }
  }, [id, setFavourite, setLoading, setMedia])
}

function useMediaDetailActions({
  media,
  navigate,
  refresh,
  setFavourite,
}: MediaDetailActionsParams) {
  const doubanEnrichmentPendingRef = useRef(false)
  const [doubanEnrichmentPending, setDoubanEnrichmentPending] = useState(false)
  const goBack = useCallback(() => goBackFromMediaDetail(media, navigate), [media, navigate])
  const toggleFavourite = useCallback(
    () => toggleMediaFavourite(media, setFavourite),
    [media, setFavourite],
  )
  const rescrape = useCallback(
    () => rescrapeMedia(media, refresh),
    [media, refresh],
  )
  const reprobe = useCallback(() => reprobeMedia(media, refresh), [media, refresh])
  const enrichDouban = useCallback(async () => {
    if (!media || doubanEnrichmentPendingRef.current) return
    doubanEnrichmentPendingRef.current = true
    setDoubanEnrichmentPending(true)
    try {
      await mediaAPI.enrichDouban(media.id)
      toast.success('豆瓣信息补齐完成')
      await refresh()
    } catch (err: unknown) {
      const status = (err as { response?: { status?: number } })?.response?.status
      toast.error(status === 429
        ? '豆瓣请求受限或暂时不可用，请稍后重试'
        : apiErrorMessage(err, '豆瓣信息补齐失败'))
    } finally {
      doubanEnrichmentPendingRef.current = false
      setDoubanEnrichmentPending(false)
    }
  }, [media, refresh])
  const softDelete = useCallback(
    () => softDeleteMedia(media, navigate),
    [media, navigate],
  )
  return { goBack, toggleFavourite, rescrape, enrichDouban, doubanEnrichmentPending, reprobe, softDelete }
}

function goBackFromMediaDetail(media: Media | null, navigate: NavigateFunction, replace = false): void {
  if (!media) return
  const backTarget = mediaLibraryBackTarget(media)
  if (backTarget) navigate(backTarget, replace ? { replace: true } : undefined)
  else navigate(-1)
}

async function toggleMediaFavourite(
  media: Media | null,
  setFavourite: Dispatch<SetStateAction<boolean>>,
): Promise<void> {
  if (!media) return
  const state = await playbackAPI.toggleFavourite(media.id)
  setFavourite(state)
  toast.success(state ? '已加入我的收藏' : '已取消收藏')
}

async function rescrapeMedia(
  media: Media | null,
  refresh: MediaDetailRefresh,
): Promise<void> {
  if (!media) return
  await api.post(`/media/${media.id}/scrape`, {
    episode_images: true,
    refresh_matched: true,
    include_matched: true,
  })
  toast.success('已触发重新刮削')
  await refresh()
}

async function reprobeMedia(media: Media | null, refresh: MediaDetailRefresh): Promise<void> {
  if (!media) return
  if (!(await confirmAction({
    title: '强制探测媒体轨',
    message: `将重新探测「${media.title}」，并覆盖已有媒体轨道信息。`,
    confirmText: '强制探测',
  }))) return
  try {
    const result = await api.post(`/media/${media.id}/probe`)
    if (result.data?.code === 0) toast.success('重新探测成功')
    else toast.error(result.data?.error || '探测失败')
    await refresh()
  } catch (err: unknown) {
    toast.error(apiErrorMessage(err, '探测失败，请检查 ffprobe 是否已安装'))
  }
}

async function softDeleteMedia(media: Media | null, navigate: NavigateFunction): Promise<void> {
  if (!media) return
  const confirmed = await confirmAction({
    title: '永久删除媒体',
    message: `将永久删除「${media.title}」的数据库记录；磁盘文件保留，此操作不可恢复。`,
    confirmText: '永久删除',
  })
  if (!confirmed) return
  await mediaAPI.delete(media.id)
  toast.success('已永久删除')
  goBackFromMediaDetail(media, navigate, true)
}

function apiErrorMessage(err: unknown, fallback: string): string {
  return (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? fallback
}
