import { useState, type ReactNode } from 'react'
import toast from 'react-hot-toast'

import { api } from '../api/client'
import { libraryAPI, mediaAPI } from '../api/library'
import { toolsAPI } from '../api/tools'
import { confirmAction } from '../components/confirmAction'
import type { Library, Media } from '../types'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'
import { LibraryMovieActions } from './LibraryMovieActions'
import { seriesSourceRoot } from './libraryPageModel'

type UseLibraryAdminActionsOptions = {
  libraryID: string
  role?: string
  library: Library | null
  selectedSeries: SeriesCard | null
  selectedSeriesEpisodes: Media[]
  reloadCurrentLibrary: () => void
  clearSelectedSeries: () => void
  setManualMovie: (media: Media | null) => void
}

export function useLibraryAdminActions({
  libraryID,
  role,
  library,
  selectedSeries,
  selectedSeriesEpisodes,
  reloadCurrentLibrary,
  clearSelectedSeries,
  setManualMovie,
}: UseLibraryAdminActionsOptions) {
  const [scraping, setScraping] = useState(false)
  const [scrapeEpisodeArtwork, setScrapeEpisodeArtwork] = useState(false)
  const [repairing, setRepairing] = useState(false)
  const [backfilling, setBackfilling] = useState(false)
	const [peopleBackfilling, setPeopleBackfilling] = useState(false)
  const [seriesToolBusy, setSeriesToolBusy] = useState('')
  const [movieToolBusy, setMovieToolBusy] = useState('')

  const handleScrape = async () => {
    setScraping(true)
    try {
      await libraryAPI.scrape(libraryID, { episode_images: scrapeEpisodeArtwork, refresh_matched: true })
      toast.success('刮削已加入后台队列')
    } catch {
      toast.error('刮削失败')
    } finally {
      setScraping(false)
    }
  }

  const handleRepairRescrape = async () => {
    if (repairing) return
    setRepairing(true)
    try {
      await toolsAPI.repairAndRescrapeLibrary(libraryID, { episode_images: scrapeEpisodeArtwork, refresh_matched: true })
      toast.success('本库修复+重刮已加入后台队列，进度可在任务中查看')
    } catch {
      toast.error('修复+重刮启动失败')
    } finally {
      setRepairing(false)
    }
  }

  const handleProbeBackfill = async () => {
    if (backfilling) return
    setBackfilling(true)
    try {
      await libraryAPI.probeTracks(libraryID)
      toast.success('媒体轨道回填已加入后台队列，进度可在任务中查看')
    } catch {
      toast.error('媒体轨道回填启动失败')
    } finally {
      setBackfilling(false)
    }
  }

  const handlePeopleBackfill = async () => {
    if (peopleBackfilling) return
    setPeopleBackfilling(true)
    try {
      await libraryAPI.backfillPeople(libraryID)
      toast.success('人物信息回填已加入后台队列，进度可在任务中查看')
    } catch {
      toast.error('人物信息回填启动失败')
    } finally {
      setPeopleBackfilling(false)
    }
  }

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

  const handleSeriesSmartScrape = () => {
    runSeriesTool('scrape', '整剧智能刮削', (media) =>
      api.post(`/media/${media.id}/scrape`, smartScrapeOptions(scrapeEpisodeArtwork)),
    )
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

  const handleSeriesOrganize = async () => {
    if (!selectedSeries || selectedSeriesEpisodes.length === 0 || !library) return
    const source = seriesSourceRoot(selectedSeriesEpisodes)
    if (!source) {
      toast.error('当前合集不是本地文件夹，无法使用本地整理入库')
      return
    }
    if (!(await confirmAction({
      title: '整理当前合集',
      message: `来源：${source}\n目标：自动按元数据选择正确分类库，当前库仅作为就近解析范围。`,
      confirmText: '开始整理',
    }))) return

    setSeriesToolBusy('organize')
    try {
      const result = await toolsAPI.organizeDirectory({
        source_path: source,
        dest_path: library.path,
        scan_after: true,
        scrape_after: true,
      })
      const replaced = result.replaced ?? 0
      const reclassified = result.reclassified ?? 0
      toast.success(`合集整理完成：新增 ${result.organized ?? 0} · 替换 ${replaced} · 纠偏 ${reclassified} · 跳过 ${result.skipped ?? 0}`)
      reloadCurrentLibrary()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '合集整理失败'
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

  const runMovieTool = async (media: Media, key: string, label: string, action: (media: Media) => Promise<unknown>) => {
    const busyKey = `${key}:${media.id}`
    setMovieToolBusy(busyKey)
    try {
      await action(media)
      toast.success(`${label}完成：${media.title}`)
      reloadCurrentLibrary()
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error || `${label}失败`
      toast.error(msg)
    } finally {
      setMovieToolBusy('')
    }
  }

  const handleMovieSmartScrape = (media: Media) => {
    runMovieTool(media, 'scrape', '智能刮削', (item) =>
      api.post(`/media/${item.id}/scrape`, smartScrapeOptions(scrapeEpisodeArtwork)),
    )
  }

  const handleMovieSoftDelete = async (media: Media) => {
    if (!(await confirmAction({
      title: '永久删除媒体',
      message: `将永久删除「${media.title}」的数据库记录；磁盘文件保留，此操作不可恢复。`,
      confirmText: '永久删除',
    }))) return
    await runMovieTool(media, 'delete', '永久删除', (item) => mediaAPI.delete(item.id))
  }

  const movieActions = (media: Media): ReactNode => {
    if (role !== 'admin') return undefined
    return (
      <LibraryMovieActions
        media={media}
        busy={movieToolBusy.endsWith(`:${media.id}`)}
        onSmartScrape={handleMovieSmartScrape}
        onManualScrape={setManualMovie}
        onSoftDelete={handleMovieSoftDelete}
      />
    )
  }

  return {
    scraping,
    scrapeEpisodeArtwork,
    repairing,
    backfilling,
    peopleBackfilling,
    seriesToolBusy,
    setScrapeEpisodeArtwork,
    handleScrape,
    handleRepairRescrape,
    handleProbeBackfill,
    handlePeopleBackfill,
    handleSeriesSmartScrape,
    handleSeriesProbe,
    handleEpisodeProbe,
    handleSeriesOrganize,
    handleSeriesSoftDelete,
    movieActions,
  }
}

function smartScrapeOptions(episodeImages: boolean) {
  return {
    episode_images: episodeImages,
    refresh_matched: true,
    include_matched: true,
  }
}
