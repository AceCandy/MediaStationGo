import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { aiAPI, type ExternalMediaResult } from '../api/ai'
import { mediaAPI } from '../api/library'
import { useAISearchAvailability } from '../components/useAISearchAvailability'
import type { Media } from '../types'
import { groupSeries } from '../utils/groupSeries'

const LOCAL_SEARCH_PAGE_SIZE = 30

function apiErrorMessage(err: unknown, fallback: string): string {
  return (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? fallback
}

export function useSearchPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const { key: locationKey } = useLocation()
  const { aiAvailable, aiChecked } = useAISearchAvailability()
  const urlQuery = searchParams.get('q') ?? ''
  const modeValues = searchParams.getAll('mode')
  const requestedMode = modeValues[0] ?? 'default'
  const validMode = modeValues.length <= 1 && (requestedMode === 'default' || requestedMode === 'ai')
  const requestedAI = validMode && requestedMode === 'ai'
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [hasSearched, setHasSearched] = useState(false)
  const [externalItems, setExternalItems] = useState<ExternalMediaResult[]>([])
  const [searchTotal, setSearchTotal] = useState(0)
  const searchSeq = useRef(0)
  const requestController = useRef<AbortController | null>(null)
  const nextPage = useRef(1)
  const pagePending = useRef(false)
  const [hasMore, setHasMore] = useState(false)
  const localCards = useMemo(() => groupSeries(items), [items])
  const aiOn = requestedAI && aiAvailable
  const normalSearchTarget = useMemo(() => {
    const next = new URLSearchParams(searchParams)
    next.delete('mode')
    const query = next.toString()
    return `/search${query ? `?${query}` : ''}`
  }, [searchParams])

  useEffect(() => {
    if (!requestedAI || !aiChecked || aiAvailable) return
    const next = new URLSearchParams(searchParams)
    next.delete('mode')
    setSearchParams(next, { replace: true })
  }, [aiAvailable, aiChecked, requestedAI, searchParams, setSearchParams])

  const doQuickSearch = useCallback((query: string, seq: number, controller: AbortController) => {
    if (pagePending.current || controller.signal.aborted) return
    pagePending.current = true
    const page = nextPage.current
    setLoading(true)
    setHasSearched(true)
    setError('')
    setExternalItems([])
    mediaAPI.searchPage(query, page, LOCAL_SEARCH_PAGE_SIZE, { signal: controller.signal })
      .then((data) => {
        if (seq !== searchSeq.current) return
        const pageItems = data.items ?? []
        const total = data.total ?? (page - 1) * LOCAL_SEARCH_PAGE_SIZE + pageItems.length
        setSearchTotal(total)
        setItems((previous) => page === 1 ? pageItems : previous.concat(pageItems))
        setHasMore(page * LOCAL_SEARCH_PAGE_SIZE < total)
        nextPage.current = page + 1
      })
      .catch((err) => {
        if (seq !== searchSeq.current) return
        const msg = apiErrorMessage(err, '搜索失败')
        setError(msg)
        toast.error(msg)
      })
      .finally(() => {
        if (seq === searchSeq.current) {
          pagePending.current = false
          setLoading(false)
        }
      })
  }, [])

  useEffect(() => {
    const seq = ++searchSeq.current
    const controller = new AbortController()
    requestController.current = controller
    nextPage.current = 1
    pagePending.current = false
    setHasMore(false)
    setItems([])
    setSearchTotal(0)
    setExternalItems([])
    setError('')
    setHasSearched(false)
    setLoading(false)
    if (!validMode || (requestedAI && !aiOn)) return
    const trimmedQuery = urlQuery.trim()
    if (!trimmedQuery) return
    setLoading(true)
    const timer = window.setTimeout(() => {
      if (!aiOn) {
        doQuickSearch(trimmedQuery, seq, controller)
        return
      }
      setHasSearched(true)
      aiAPI.smartSearch(trimmedQuery, controller.signal)
        .then((data) => {
          if (seq !== searchSeq.current) return
          setItems(data.items ?? [])
          setSearchTotal((data.items ?? []).length)
          setExternalItems(data.external_items ?? [])
        })
        .catch((err) => {
          if (seq !== searchSeq.current) return
          const msg = apiErrorMessage(err, 'AI 搜索失败')
          setError(msg)
          toast.error(msg)
        })
        .finally(() => { if (seq === searchSeq.current) setLoading(false) })
    }, 300)
    return () => {
      window.clearTimeout(timer)
      searchSeq.current = seq + 1
      controller.abort()
    }
  }, [urlQuery, locationKey, aiOn, doQuickSearch, requestedAI, validMode])

  return {
    error,
    externalItems,
    itemCount: items.length,
    loading,
    hasMore,
    loadMore: () => {
      const controller = requestController.current
      if (hasMore && !aiOn && controller) doQuickSearch(urlQuery.trim(), searchSeq.current, controller)
    },
    localCards,
    normalizationTarget: validMode ? null : normalSearchTarget,
    searchTotal,
    showEmpty: !loading && !error && !hasMore && hasSearched && localCards.length === 0,
    showIdle: !loading && !error && !hasSearched,
  }
}
