import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { useSearchParams } from 'react-router-dom'
import { useAuthStore } from '../stores/auth'
import { usePlayProfileStore } from '../stores/playProfile'

import { libraryAPI, type LibraryMediaFilters } from '../api/library'
import type { Library, Media } from '../types'
import type { HistoryItem } from '../types/history'
import { groupSeries, isEpisodeLike, type SeriesCard } from '../utils/groupSeries'
import { isSeriesLibraryType } from './librariesPageModel'

const LIBRARY_PAGE_SIZE = 50

export function useLibraryData(libraryID: string, filters: LibraryMediaFilters) {
  const [searchParams] = useSearchParams()
  const userID = useAuthStore((state) => state.user?.id)
  const profileID = usePlayProfileStore((state) => state.activeProfileId)
  const seriesID = searchParams.get('series_id') || ''
  const seriesKey = seriesID ? '' : searchParams.get('series') || ''
  const target = `${userID}:${profileID}:${libraryID}:${seriesID}:${seriesKey}`
  const [linkedSeries, setLinkedSeries] = useState<{ target: string; card: SeriesCard | null } | null>(null)
  const { missingPoster = false, missingChineseTitle = false } = filters
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [serverSeriesCards, setServerSeriesCards] = useState<SeriesCard[]>([])
  const [seriesEpisodeItems, setSeriesEpisodeItems] = useState<Media[]>([])
  const [seriesHistory, setSeriesHistory] = useState<HistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState(false)
  const [loadingSeriesEpisodes, setLoadingSeriesEpisodes] = useState(false)
  const [seriesEpisodesError, setSeriesEpisodesError] = useState(false)
  const [nextPage, setNextPage] = useState(2)
  const loadingMoreRef = useRef(false)
  const loadVersionRef = useRef(0)

  const isSeriesLibrary = isSeriesLibraryType(library?.type)
  const episodeKey = seriesID ? `metadata:${seriesID}` : seriesKey
  const isSeriesDetail = isSeriesLibrary && !!episodeKey
  const hasEpisodicItems = useMemo(() => items.some(isEpisodeLike), [items])
  const isSeries = isSeriesLibrary || serverSeriesCards.length > 0 || hasEpisodicItems

  const seriesCards = useMemo(() => {
    if (isSeriesLibrary) {
      const card = linkedSeries?.target === target ? linkedSeries.card : null
      return card && !serverSeriesCards.some((item) => item.key === card.key) ? [...serverSeriesCards, card] : serverSeriesCards
    }
    if (!isSeries || items.length === 0) return []
    return groupSeries(items)
  }, [isSeries, isSeriesLibrary, items, serverSeriesCards, linkedSeries, target])

  useEffect(() => {
    if (!library || !isSeriesLibrary || (!seriesID && !seriesKey)) return
    let cancelled = false
    libraryAPI.listSeries(libraryID, 1, 1, { seriesID, key: seriesKey })
      .then((result) => { if (!cancelled) setLinkedSeries({ target, card: result.items[0] ?? null }) })
      .catch(() => {
        if (!cancelled) {
          setLinkedSeries({ target, card: null })
          toast.error('剧集详情加载失败，请刷新重试')
        }
      })
    return () => { cancelled = true }
  }, [library, libraryID, isSeriesLibrary, seriesID, seriesKey, target])

  useEffect(() => {
    if (!libraryID) return
    let cancelled = false
    setLoading(true)
    setLibrary(null)
    setItems([])
    setServerSeriesCards([])
    setSeriesEpisodeItems([])
    libraryAPI.get(libraryID)
      .then((lib) => {
        if (!cancelled) setLibrary(lib)
      })
      .catch(() => {
        if (!cancelled) {
          setLibrary(null)
          setLoading(false)
          toast.error('媒体库不存在或无权限')
        }
      })
    return () => { cancelled = true }
  }, [libraryID, userID, profileID])

  useEffect(() => {
    if (!libraryID || !library) return
    let cancelled = false
    loadVersionRef.current += 1
    loadingMoreRef.current = false
    setLoading(true)
    setLoadingMore(false)
    setLoadMoreError(false)
    setTotal(0)
    setNextPage(2)
    setItems([])
    setServerSeriesCards([])

    if (isSeriesDetail) {
      setLoading(false)
      return
    }

    loadLibraryPage(libraryID, isSeriesLibrary, 1, { missingPoster, missingChineseTitle })
      .then((page) => {
        if (cancelled) return
        setTotal(page.total)
        if (page.kind === 'series') setServerSeriesCards(page.items)
        else setItems(page.items)
      })
      .catch(() => {
        if (!cancelled) toast.error('媒体库加载失败')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [missingChineseTitle, missingPoster, libraryID, library, isSeriesLibrary, isSeriesDetail])

  const loadedCount = isSeriesLibrary ? serverSeriesCards.length : items.length
  const hasMore = loadedCount < total
  const loadMore = useCallback(async () => {
    if (!libraryID || !library || !hasMore || loadingMoreRef.current) return
    const loadVersion = loadVersionRef.current
    loadingMoreRef.current = true
    setLoadingMore(true)
    setLoadMoreError(false)
    try {
      const page = await loadLibraryPage(libraryID, isSeriesLibrary, nextPage, { missingPoster, missingChineseTitle })
      if (loadVersion !== loadVersionRef.current) return
      setTotal(page.items.length === 0 ? loadedCount : page.total)
      if (page.kind === 'series') setServerSeriesCards((current) => current.concat(page.items))
      else setItems((current) => current.concat(page.items))
      setNextPage((current) => current + 1)
    } catch {
      if (loadVersion === loadVersionRef.current) {
        setLoadMoreError(true)
        toast.error('加载更多失败')
      }
    } finally {
      if (loadVersion === loadVersionRef.current) {
        loadingMoreRef.current = false
        setLoadingMore(false)
      }
    }
  }, [missingChineseTitle, missingPoster, hasMore, isSeriesLibrary, library, libraryID, loadedCount, nextPage])

  useEffect(() => {
    setSeriesHistory([])
    setSeriesEpisodesError(false)
    if (!libraryID || !isSeriesLibrary || !episodeKey) {
      setSeriesEpisodeItems([])
      setLoadingSeriesEpisodes(false)
      return
    }
    let cancelled = false
    setLoadingSeriesEpisodes(true)
    setSeriesEpisodeItems([])
    libraryAPI.listSeriesEpisodes(libraryID, episodeKey)
      .then((r) => {
        if (!cancelled) {
          setSeriesEpisodeItems(r.items ?? [])
          setSeriesHistory(r.history ?? [])
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSeriesEpisodesError(true)
          toast.error('剧集列表加载失败')
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingSeriesEpisodes(false)
      })
    return () => { cancelled = true }
  }, [libraryID, library, isSeriesLibrary, episodeKey, userID, profileID])

  const reloadCurrentLibrary = useCallback(() => {
    setLibrary((current) => (current ? { ...current } : current))
  }, [])

  const loadingAllText = loadingMore
    ? `正在加载更多：${loadedCount} / ${total}`
    : ''

  return {
    library,
    items,
    seriesEpisodeItems,
    seriesHistory,
    total,
    loading: loading || (isSeriesLibrary && !!(seriesID || seriesKey) && linkedSeries?.target !== target),
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
  }
}

async function loadLibraryPage(libraryID: string, series: boolean, page: number, filters: LibraryMediaFilters) {
  if (series) {
    const data = await libraryAPI.listSeries(libraryID, page, LIBRARY_PAGE_SIZE, filters)
    const items = data.items ?? []
    return { kind: 'series' as const, items, total: data.total ?? items.length }
  }
  const data = await libraryAPI.listMedia(libraryID, page, LIBRARY_PAGE_SIZE, filters)
  const items = data.items ?? []
  return { kind: 'media' as const, items, total: data.total ?? items.length }
}
