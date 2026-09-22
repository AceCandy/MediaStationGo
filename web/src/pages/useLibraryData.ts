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
  const sessionVersion = useAuthStore((state) => state.sessionVersion)
  const profileID = usePlayProfileStore((state) => state.activeProfileId)
  const seriesID = searchParams.get('series_id') || ''
  const seriesKey = seriesID ? '' : searchParams.get('series') || ''
  const rawSeason = searchParams.get('season')
  const requestedSeason = rawSeason !== null && /^\d+$/.test(rawSeason) ? Number(rawSeason) : undefined
  const target = `${sessionVersion}:${userID}:${profileID}:${libraryID}:${seriesID}:${seriesKey}`
  const [linkedSeries, setLinkedSeries] = useState<{ target: string; card: SeriesCard | null } | null>(null)
  const { missingPoster = false, missingChineseTitle = false } = filters
  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [serverSeriesCards, setServerSeriesCards] = useState<SeriesCard[]>([])
  const [seriesEpisodeItems, setSeriesEpisodeItems] = useState<Media[]>([])
  const [seriesHistory, setSeriesHistory] = useState<HistoryItem[]>([])
  const [seriesResume, setSeriesResume] = useState<HistoryItem | null>(null)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState(false)
  const [loadingSeriesEpisodes, setLoadingSeriesEpisodes] = useState(false)
  const [seriesEpisodesError, setSeriesEpisodesError] = useState(false)
  const [nextPage, setNextPage] = useState(2)
  const loadingMoreRef = useRef(false)
  const loadVersionRef = useRef(0)
  const loadMoreController = useRef<AbortController | null>(null)

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
    const controller = new AbortController()
    libraryAPI.listSeries(libraryID, 1, 1, { seriesID, key: seriesKey, signal: controller.signal })
      .then((result) => { if (!cancelled) setLinkedSeries({ target, card: result.items[0] ?? null }) })
      .catch(() => {
        if (!cancelled) {
          setLinkedSeries({ target, card: null })
          toast.error('剧集详情加载失败，请刷新重试')
        }
      })
    return () => { cancelled = true; controller.abort() }
  }, [library, libraryID, isSeriesLibrary, seriesID, seriesKey, target])

  useEffect(() => {
    if (!libraryID) return
    let cancelled = false
    const controller = new AbortController()
    setLoading(true)
    setLibrary(null)
    setItems([])
    setServerSeriesCards([])
    setSeriesEpisodeItems([])
    libraryAPI.get(libraryID, { signal: controller.signal })
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
    return () => { cancelled = true; controller.abort() }
  }, [libraryID, userID, profileID, sessionVersion])

  useEffect(() => {
    if (!libraryID || !library) return
    let cancelled = false
    const controller = new AbortController()
    loadVersionRef.current += 1
    loadingMoreRef.current = false
    setLoading(true)
    setLoadingMore(false)
    setLoadMoreError(false)
    setTotal(0)
    setNextPage(2)
    setItems([])
    setServerSeriesCards([])

    if (isSeriesDetail || library.type === 'hongguo') {
      setLoading(false)
      return
    }

    loadLibraryPage(libraryID, isSeriesLibrary, 1, { missingPoster, missingChineseTitle }, controller.signal)
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
    return () => {
      cancelled = true
      controller.abort()
      loadVersionRef.current += 1
      loadMoreController.current?.abort()
    }
  }, [missingChineseTitle, missingPoster, libraryID, library, isSeriesLibrary, isSeriesDetail, sessionVersion, userID, profileID])

  const loadedCount = isSeriesLibrary ? serverSeriesCards.length : items.length
  const hasMore = loadedCount < total
  const loadMore = useCallback(async () => {
    if (!libraryID || !library || !hasMore || loadingMoreRef.current) return
    const loadVersion = loadVersionRef.current
    const controller = new AbortController()
    loadMoreController.current = controller
    loadingMoreRef.current = true
    setLoadingMore(true)
    setLoadMoreError(false)
    try {
      const page = await loadLibraryPage(libraryID, isSeriesLibrary, nextPage, { missingPoster, missingChineseTitle }, controller.signal)
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
    setSeriesResume(null)
    setSeriesEpisodesError(false)
    if (!libraryID || !isSeriesLibrary || !episodeKey) {
      setSeriesEpisodeItems([])
      setLoadingSeriesEpisodes(false)
      return
    }
    let cancelled = false
    const controller = new AbortController()
    setLoadingSeriesEpisodes(true)
    setSeriesEpisodeItems([])
    libraryAPI.listSeriesEpisodes(libraryID, episodeKey, requestedSeason, controller.signal)
      .then((r) => {
        if (!cancelled) {
          setSeriesEpisodeItems(r.items ?? [])
          setSeriesHistory(r.history ?? [])
          setSeriesResume(r.resume ?? null)
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
    return () => { cancelled = true; controller.abort() }
  }, [libraryID, library, isSeriesLibrary, episodeKey, requestedSeason, userID, profileID, sessionVersion])

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
    seriesResume,
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

async function loadLibraryPage(libraryID: string, series: boolean, page: number, filters: LibraryMediaFilters, signal: AbortSignal) {
  if (series) {
    const data = await libraryAPI.listSeries(libraryID, page, LIBRARY_PAGE_SIZE, { ...filters, signal })
    const items = data.items ?? []
    return { kind: 'series' as const, items, total: data.total ?? items.length }
  }
  const data = await libraryAPI.listMedia(libraryID, page, LIBRARY_PAGE_SIZE, { ...filters, signal })
  const items = data.items ?? []
  return { kind: 'media' as const, items, total: data.total ?? items.length }
}
