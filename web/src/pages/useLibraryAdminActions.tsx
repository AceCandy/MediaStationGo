import { useState } from 'react'
import toast from 'react-hot-toast'

import { api } from '../api/client'
import { libraryAPI, mediaAPI } from '../api/library'
import { confirmAction } from '../components/confirmAction'
import type { Media } from '../types'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'

type UseLibraryAdminActionsOptions = {
  libraryID: string
  selectedSeries: SeriesCard | null
  reloadCurrentLibrary: () => void
  clearSelectedSeries: () => void
}

export function useLibraryAdminActions({
  libraryID,
  selectedSeries,
  reloadCurrentLibrary,
  clearSelectedSeries,
}: UseLibraryAdminActionsOptions) {
  const [seriesToolBusy, setSeriesToolBusy] = useState('')

  const runSeriesTool = async (key: string, label: string, action: (media: Media) => Promise<unknown>) => {
    if (!selectedSeries) return false
    setSeriesToolBusy(key)
    try {
      const { items: episodes } = await libraryAPI.listSeriesEpisodes(libraryID, selectedSeries.key)
      if (!episodes?.length) return false
      for (const ep of episodes) {
        await action(ep)
      }
      toast.success(`${label}完成：${episodes.length} 个媒体`)
      reloadCurrentLibrary()
      return true
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || `${label}失败`
      toast.error(msg)
      return false
    } finally {
      setSeriesToolBusy('')
    }
  }

  const handleSeriesProbe = async () => {
    if (!(await confirmAction({
      title: '强制探测整剧',
      message: `将重新探测整剧媒体，并覆盖已有媒体轨道信息。`,
      confirmText: '强制探测',
    }))) return
    await runSeriesTool('probe', '整剧媒体轨探测', (media) => api.post(`/media/${media.id}/probe`))
  }

  const handleEpisodeProbe = async (media: Media) => {
    if (!(await confirmAction({
      title: '强制探测单集',
      message: `将重新探测「${media.title}」，并覆盖已有媒体轨道信息。`,
      confirmText: '强制探测',
    }))) return
    setSeriesToolBusy(`probe:${media.id}`)
    try {
      await api.post(`/media/${media.id}/probe`)
      toast.success(`单集媒体轨探测完成：${media.title}`)
      reloadCurrentLibrary()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '单集媒体轨探测失败'
      toast.error(msg)
    } finally {
      setSeriesToolBusy('')
    }
  }

  const handleSeriesSoftDelete = async () => {
    if (!selectedSeries) return
    if (!(await confirmAction({
      title: '永久删除媒体',
      message: `将永久删除「${seriesTitle(selectedSeries.rep)}」的整剧数据库记录；磁盘文件保留，此操作不可恢复。`,
      confirmText: '永久删除',
    }))) return
    if (await runSeriesTool('delete', '整剧永久删除', (media) => mediaAPI.delete(media.id))) clearSelectedSeries()
  }

  return {
    seriesToolBusy,
    handleSeriesProbe,
    handleEpisodeProbe,
    handleSeriesSoftDelete,
  }
}
