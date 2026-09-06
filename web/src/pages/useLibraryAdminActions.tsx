import { useState } from 'react'
import toast from 'react-hot-toast'

import { api } from '../api/client'
import { mediaAPI } from '../api/library'
import { confirmAction } from '../components/confirmAction'
import type { Media } from '../types'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'

type UseLibraryAdminActionsOptions = {
  selectedSeries: SeriesCard | null
  selectedSeriesEpisodes: Media[]
  reloadCurrentLibrary: () => void
  clearSelectedSeries: () => void
}

export function useLibraryAdminActions({
  selectedSeries,
  selectedSeriesEpisodes,
  reloadCurrentLibrary,
  clearSelectedSeries,
}: UseLibraryAdminActionsOptions) {
  const [seriesToolBusy, setSeriesToolBusy] = useState('')

  const runSeriesTool = async (key: string, label: string, action: (media: Media) => Promise<unknown>) => {
    if (selectedSeriesEpisodes.length === 0) return
    setSeriesToolBusy(key)
    try {
      for (const ep of selectedSeriesEpisodes) {
        await action(ep)
      }
      toast.success(`${label}完成：${selectedSeriesEpisodes.length} 个媒体`)
      reloadCurrentLibrary()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || `${label}失败`
      toast.error(msg)
    } finally {
      setSeriesToolBusy('')
    }
  }

  const handleSeriesProbe = async () => {
    if (!(await confirmAction({
      title: '强制探测整剧',
      message: `将重新探测 ${selectedSeriesEpisodes.length} 个媒体，并覆盖已有媒体轨道信息。`,
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
    if (!selectedSeries || selectedSeriesEpisodes.length === 0) return
    if (!(await confirmAction({
      title: '永久删除媒体',
      message: `将永久删除「${seriesTitle(selectedSeries.rep)}」的 ${selectedSeriesEpisodes.length} 条数据库记录；磁盘文件保留，此操作不可恢复。`,
      confirmText: '永久删除',
    }))) return
    await runSeriesTool('delete', '整剧永久删除', (media) => mediaAPI.delete(media.id))
    clearSelectedSeries()
  }

  return {
    seriesToolBusy,
    handleSeriesProbe,
    handleEpisodeProbe,
    handleSeriesSoftDelete,
  }
}
