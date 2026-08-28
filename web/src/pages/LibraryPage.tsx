import { useEffect, useRef, useState } from 'react'
import { useLocation, useParams, useSearchParams } from 'react-router-dom'
import { motion } from 'framer-motion'
import toast from 'react-hot-toast'

import type { Media } from '../types'
import { playbackAPI } from '../api/playback'
import { useAuthStore } from '../stores/auth'
import type { SeriesCard } from '../utils/groupSeries'
import { LibraryPageDialogs } from './LibraryPageDialogs'
import { LibraryPageHeader } from './LibraryPageHeader'
import { LibraryMediaSections } from './LibraryMediaSections'
import { LibrarySeriesDetailSection } from './LibrarySeriesDetailSection'
import { useLibraryData } from './useLibraryData'
import { useLibrarySeriesSelection } from './useLibrarySeriesSelection'
import { useLibraryAdminActions } from './useLibraryAdminActions'

export function LibraryPage() {
  const { id = '' } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const location = useLocation()
  const role = useAuthStore((s) => s.user?.role)

  const [manualSeriesScrapeOpen, setManualSeriesScrapeOpen] = useState(false)
  const [seriesMetadataEditOpen, setSeriesMetadataEditOpen] = useState(false)
  const [missingPoster, setMissingPoster] = useState(false)
  const [missingChineseTitle, setMissingChineseTitle] = useState(false)

  // 网格卡片收藏：整页拉一次收藏列表（API 层有 5s 缓存），本地维护 id 集合
  const [favouriteIds, setFavouriteIds] = useState<ReadonlySet<string>>(() => new Set())

  useEffect(() => {
    let cancelled = false
    playbackAPI
      .listFavourites()
      .then((list) => {
        if (!cancelled) setFavouriteIds(new Set(list.map((item) => item.id)))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  const handleToggleFavourite = (media: Media) => {
    playbackAPI
      .toggleFavourite(media.id)
      .then((state) => {
        setFavouriteIds((prev) => {
          const next = new Set(prev)
          if (state) next.add(media.id)
          else next.delete(media.id)
          return next
        })
        toast.success(state ? '已加入我的收藏' : '已取消收藏')
      })
      .catch(() => toast.error('收藏操作失败'))
  }

  // 剧集模式：选中某个剧集后展开详情
  const [selectedSeries, setSelectedSeries] = useState<SeriesCard | null>(null)
  const [selectedSeason, setSelectedSeason] = useState<number | null>(null)

  const {
    library,
    items,
    seriesEpisodeItems,
    total,
    loading,
    loadingSeriesEpisodes,
    isSeriesLibrary,
    isSeries,
    seriesCards,
    loadingAllText,
    loadingMore,
    loadMoreError,
    hasMore,
    loadMore,
    reloadCurrentLibrary,
  } = useLibraryData(id, selectedSeries, { missingPoster, missingChineseTitle })
  const loadMoreRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const target = loadMoreRef.current
    if (!target || !hasMore || loadingMore || loadMoreError || selectedSeries) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) void loadMore()
    }, { rootMargin: '600px 0px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [hasMore, loadMore, loadingMore, loadMoreError, selectedSeries])

  const {
    selectedEpisodes,
    visibleEpisodes,
    selectedSeriesEpisodes,
    selectedSeriesMediaIDs,
    handleSeriesClick,
    clearSelectedSeries,
  } = useLibrarySeriesSelection({
    items,
    seriesEpisodeItems,
    isSeriesLibrary,
    isSeries,
    loading,
    seriesCards,
    searchParams,
    setSearchParams,
    selectedSeries,
    setSelectedSeries,
    selectedSeason,
    setSelectedSeason,
    onClearSeriesState: () => setSeriesMetadataEditOpen(false),
  })

  const {
    seriesToolBusy,
    handleSeriesSmartScrape,
    handleSeriesProbe,
    handleEpisodeProbe,
    handleSeriesOrganize,
    handleSeriesSoftDelete,
  } = useLibraryAdminActions({
    library,
    selectedSeries,
    selectedSeriesEpisodes,
    reloadCurrentLibrary,
    clearSelectedSeries,
  })

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 2 }} className="flex items-center gap-3">
          <div className="h-2 w-2 rounded-full bg-brand-500" />
          <span className="text-sm text-sand-500">加载中…</span>
        </motion.div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <LibraryPageHeader
        library={library}
        itemCount={isSeriesLibrary ? total : isSeries ? seriesCards.length : total}
        loadingAllText={loadingAllText}
        isAdmin={role === 'admin'}
        missingPoster={missingPoster}
        missingChineseTitle={missingChineseTitle}
        onMissingPosterChange={setMissingPoster}
        onMissingChineseTitleChange={setMissingChineseTitle}
      />

      <LibraryMediaSections
        isSeries={isSeries}
        items={items}
        seriesCards={seriesCards}
        selectedSeries={selectedSeries}
        loading={loading}
        filtered={missingPoster || missingChineseTitle}
        favouriteIds={favouriteIds}
        onToggleFavourite={handleToggleFavourite}
        onSeriesClick={handleSeriesClick}
      />

      {!selectedSeries && hasMore && (
        <div ref={loadMoreRef} className="py-4 text-center text-sm text-sand-500">
          {loadMoreError ? (
            <button type="button" className="neon-button min-h-11" onClick={() => void loadMore()}>
              重试加载更多
            </button>
          ) : loadingMore ? '正在加载更多…' : '继续下滑加载更多'}
        </div>
      )}

      <LibrarySeriesDetailSection
        selectedSeries={selectedSeries}
        selectedEpisodes={selectedEpisodes}
        selectedSeason={selectedSeason}
        visibleEpisodes={visibleEpisodes}
        allEpisodes={selectedSeriesEpisodes}
        loadingEpisodes={loadingSeriesEpisodes}
        playbackFrom={`${location.pathname}${location.search}`}
        isAdmin={role === 'admin'}
        seriesToolBusy={seriesToolBusy}
        onBack={clearSelectedSeries}
        onSmartScrape={handleSeriesSmartScrape}
        onManualScrape={() => setManualSeriesScrapeOpen(true)}
        onMetadataEdit={() => setSeriesMetadataEditOpen(true)}
        onProbe={handleSeriesProbe}
        onEpisodeProbe={handleEpisodeProbe}
        onOrganize={handleSeriesOrganize}
        onSoftDelete={handleSeriesSoftDelete}
        onSeasonChange={setSelectedSeason}
      />

      <LibraryPageDialogs
        manualSeriesScrapeOpen={manualSeriesScrapeOpen}
        seriesMetadataEditOpen={seriesMetadataEditOpen}
        selectedSeries={selectedSeries}
        selectedSeriesMediaIDs={selectedSeriesMediaIDs}
        libraryType={library?.type}
        onCloseManualSeriesScrape={() => setManualSeriesScrapeOpen(false)}
        onCloseSeriesMetadataEdit={() => setSeriesMetadataEditOpen(false)}
        onApplied={reloadCurrentLibrary}
      />
    </div>
  )
}
