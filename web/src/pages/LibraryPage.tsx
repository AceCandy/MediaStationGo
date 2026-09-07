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

  const [seriesMetadataEditOpen, setSeriesMetadataEditOpen] = useState(false)
  const missingPoster = searchParams.get('missing_poster') === '1'
  const missingChineseTitle = searchParams.get('missing_chinese_title') === '1'

  const setFilter = (key: 'missing_poster' | 'missing_chinese_title', enabled: boolean) => {
    const next = new URLSearchParams(searchParams)
    if (enabled) next.set(key, '1')
    else next.delete(key)
    setSearchParams(next)
  }

  // 网格卡片收藏：整页拉一次收藏列表（API 层有 5s 缓存），本地维护 id 集合
  const [favouriteIds, setFavouriteIds] = useState<ReadonlySet<string>>(() => new Set())

  useEffect(() => {
    let cancelled = false
    playbackAPI
      .listFavourites()
      .then((list) => {
        if (!cancelled) setFavouriteIds(new Set(list.map((item) => item.metadata_id || item.id)))
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
          if (state) next.add(media.metadata_id || media.id)
          else next.delete(media.metadata_id || media.id)
          return next
        })
        toast.success(state ? '已加入我的收藏' : '已取消收藏')
      })
      .catch(() => toast.error('收藏操作失败'))
  }

  // 剧集模式：选中某个剧集后展开详情
  const [selectedSeries, setSelectedSeries] = useState<SeriesCard | null>(null)

  const {
    library,
    items,
    seriesEpisodeItems,
    seriesHistory,
    total,
    loading,
    loadingSeriesEpisodes,
    seriesEpisodesError,
    isSeriesLibrary,
    isSeries,
    seriesCards,
    loadingAllText,
    loadingMore,
    loadMoreError,
    hasMore,
    loadMore,
    reloadCurrentLibrary,
  } = useLibraryData(id, { missingPoster, missingChineseTitle })
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
    selectedSeriesEpisodes,
    handleSeriesClick,
    handleSeasonChange,
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
    onClearSeriesState: () => setSeriesMetadataEditOpen(false),
  })

  const {
    seriesToolBusy,
    handleSeriesProbe,
    handleSeriesSoftDelete,
  } = useLibraryAdminActions({
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

  if ((searchParams.has('series_id') || searchParams.has('series')) && !selectedSeries) {
    return <div className="space-y-4 rounded-2xl border border-[var(--app-border)] p-6"><p role="status" className="text-[var(--app-muted)]">剧集不存在、加载失败或当前账号无权查看。</p><div className="flex gap-3"><button className="btn-outline" onClick={reloadCurrentLibrary}>重试</button><button className="btn-ghost" onClick={clearSelectedSeries}>返回媒体库</button></div></div>
  }

  return (
    <div className="space-y-6">
      {!selectedSeries && <LibraryPageHeader
        library={library}
        itemCount={isSeriesLibrary ? total : isSeries ? seriesCards.length : total}
        loadingAllText={loadingAllText}
        isAdmin={role === 'admin'}
        missingPoster={missingPoster}
        missingChineseTitle={missingChineseTitle}
        onMissingPosterChange={(enabled) => setFilter('missing_poster', enabled)}
        onMissingChineseTitleChange={(enabled) => setFilter('missing_chinese_title', enabled)}
      />}

      <LibraryMediaSections
        isSeries={isSeries}
        items={items}
        seriesCards={seriesCards}
        selectedSeries={selectedSeries}
        loading={loading}
        filtered={missingPoster || missingChineseTitle}
        detailFrom={`${location.pathname}${location.search}`}
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
        key={selectedSeries?.key}
        selectedSeries={selectedSeries}
        selectedEpisodes={selectedEpisodes}
        allEpisodes={selectedSeriesEpisodes}
        history={seriesHistory}
        loadingEpisodes={loadingSeriesEpisodes}
        episodesError={seriesEpisodesError}
        playbackFrom={`${location.pathname}${location.search}`}
        isAdmin={role === 'admin'}
        seriesToolBusy={seriesToolBusy}
        onBack={clearSelectedSeries}
        onMetadataEdit={() => setSeriesMetadataEditOpen(true)}
        onProbe={handleSeriesProbe}
        onChanged={reloadCurrentLibrary}
        onSoftDelete={handleSeriesSoftDelete}
        onSeasonChange={handleSeasonChange}
      />

      <LibraryPageDialogs
        seriesMetadataEditOpen={seriesMetadataEditOpen}
        selectedSeries={selectedSeries}
        onCloseSeriesMetadataEdit={() => setSeriesMetadataEditOpen(false)}
        onApplied={reloadCurrentLibrary}
      />
    </div>
  )
}
