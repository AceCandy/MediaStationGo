import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { aiAPI, type ExternalMediaResult } from '../api/ai'
import { mediaAPI } from '../api/library'
import { useAISearchAvailability } from '../components/useAISearchAvailability'
import type { Media } from '../types'
import { groupSeries } from '../utils/groupSeries'

const LOCAL_SEARCH_PAGE_SIZE = 2000

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

  const doQuickSearch = useCallback((query: string, seq: number) => {
    if (!query.trim()) {
      setItems([])
      setSearchTotal(0)
      setHasSearched(false)
      setLoading(false)
      return
    }

    setHasSearched(true)
    setError('')
    setExternalItems([])
    const loadAll = async () => {
      let page = 1
      let collected: Media[] = []
      for (;;) {
        const data = await mediaAPI.searchPage(query, page, LOCAL_SEARCH_PAGE_SIZE)
        if (seq !== searchSeq.current) return
        const pageItems = data.items ?? []
        collected = collected.concat(pageItems)
        const total = data.total ?? collected.length
        setSearchTotal(total)
        if (page === 1) setItems(collected)
        if (collected.length >= total || pageItems.length < LOCAL_SEARCH_PAGE_SIZE) break
        page += 1
      }
      if (seq !== searchSeq.current) return
      setItems(collected)
      setExternalItems([])
    }
    loadAll()
      .catch((err) => {
        if (seq !== searchSeq.current) return
        const msg = apiErrorMessage(err, '搜索失败')
        setError(msg)
        toast.error(msg)
      })
      .finally(() => {
        if (seq === searchSeq.current) setLoading(false)
      })
  }, [])

  useEffect(() => {
    const seq = ++searchSeq.current
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
        doQuickSearch(trimmedQuery, seq)
        return
      }
      setHasSearched(true)
      aiAPI.smartSearch(trimmedQuery)
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
    }
  }, [urlQuery, locationKey, aiOn, doQuickSearch, requestedAI, validMode])

  return {
    error,
    externalItems,
    itemCount: items.length,
    loading,
    localCards,
    normalizationTarget: validMode ? null : normalSearchTarget,
    searchTotal,
    showEmpty: !loading && !error && hasSearched && localCards.length === 0,
    showIdle: !loading && !error && !hasSearched,
  }
}
