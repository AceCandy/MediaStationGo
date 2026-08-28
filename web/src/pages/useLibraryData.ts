import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import toast from 'react-hot-toast'

import { libraryAPI, type LibraryMediaFilters } from '../api/library'
import type { Library, Media } from '../types'
import { groupSeries, isEpisodeLike, type SeriesCard } from '../utils/groupSeries'
import { isSeriesLibraryType } from './librariesPageModel'

const LIBRARY_PAGE_SIZE = 50

export function useLibraryData(libraryID: string, selectedSeries: SeriesCard | null, filters: LibraryMediaFilters) {
  const { missingPoster = false, missingChineseTitle = false } = filters
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [serverSeriesCards, setServerSeriesCards] = useState<SeriesCard[]>([])
  const [seriesEpisodeItems, setSeriesEpisodeItems] = useState<Media[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState(false)
  const [loadingSeriesEpisodes, setLoadingSeriesEpisodes] = useState(false)
  const [nextPage, setNextPage] = useState(2)
  const loadingMoreRef = useRef(false)
  const loadVersionRef = useRef(0)

  const isSeriesLibrary = isSeriesLibraryType(library?.type)
  const hasEpisodicItems = useMemo(() => items.some(isEpisodeLike), [items])
  const isSeries = isSeriesLibrary || serverSeriesCards.length > 0 || hasEpisodicItems

  const seriesCards = useMemo(() => {
    if (isSeriesLibrary) return serverSeriesCards
    if (!isSeries || items.length === 0) return []
    return groupSeries(items)
  }, [isSeries, isSeriesLibrary, items, serverSeriesCards])

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
  }, [libraryID])

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
    setSeriesEpisodeItems([])

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
  }, [missingChineseTitle, missingPoster, libraryID, library, isSeriesLibrary])

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
    if (!libraryID || !isSeriesLibrary || !selectedSeries) {
      setSeriesEpisodeItems([])
      setLoadingSeriesEpisodes(false)
      return
    }
    let cancelled = false
    setLoadingSeriesEpisodes(true)
    setSeriesEpisodeItems([])
    libraryAPI.listSeriesEpisodes(libraryID, selectedSeries.key)
      .then((r) => {
        if (!cancelled) setSeriesEpisodeItems(r.items ?? [])
      })
      .catch(() => {
        if (!cancelled) toast.error('剧集列表加载失败')
      })
      .finally(() => {
        if (!cancelled) setLoadingSeriesEpisodes(false)
      })
    return () => { cancelled = true }
  }, [libraryID, isSeriesLibrary, selectedSeries])

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
