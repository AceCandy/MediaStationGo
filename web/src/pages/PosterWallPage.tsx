import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { GalleryHorizontalEnd, Layers } from 'lucide-react'

import { libraryAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Library, Media } from '../types'
import { groupSeries, seriesCardLink } from '../utils/groupSeries'

const POSTER_PAGE_SIZE = 50
const REQUESTS_PER_BATCH = 3

type PosterLibraryState = {
  library: Library
  items: Media[]
  nextPage: number
  received: number
  total: number | null
  complete: boolean
  error: string
}

type PosterPageResult = {
  libraryID: string
  items?: Media[]
  total?: number
  error?: string
}

export function PosterWallPage() {
  const [libraries, setLibraries] = useState<PosterLibraryState[]>([])
  const [loading, setLoading] = useState(true)
  const [catalogError, setCatalogError] = useState('')
  const rowsRef = useRef<PosterLibraryState[]>([])
  const cursorRef = useRef(0)
  const activeRef = useRef(true)
  const loadingRef = useRef(false)

  const storeRows = useCallback((rows: PosterLibraryState[]) => {
    rowsRef.current = rows
    if (activeRef.current) setLibraries(rows)
  }, [])

  const loadBatch = useCallback(async (source?: PosterLibraryState[]) => {
    if (loadingRef.current) return
    const rows = source ?? rowsRef.current
    const selected = selectPosterBatch(rows, cursorRef.current)
    if (selected.indexes.length === 0) {
      if (activeRef.current) setLoading(false)
      return
    }

    cursorRef.current = selected.nextCursor
    loadingRef.current = true
    if (activeRef.current) setLoading(true)
    const results = await Promise.all(selected.indexes.map(async (index): Promise<PosterPageResult> => {
      const row = rows[index]
      try {
        const page = await libraryAPI.listMedia(row.library.id, row.nextPage, POSTER_PAGE_SIZE, {
          groupVersions: false,
        })
        return { libraryID: row.library.id, items: page.items ?? [], total: page.total }
      } catch (error) {
        return {
          libraryID: row.library.id,
          error: (error as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '加载失败',
        }
      }
    }))

    if (activeRef.current) {
      const resultByLibrary = new Map(results.map((result) => [result.libraryID, result]))
      const nextRows = rowsRef.current.map((row) => {
        const result = resultByLibrary.get(row.library.id)
        if (!result) return row
        if (result.error) return { ...row, error: result.error }

        const pageItems = result.items ?? []
        const received = row.received + pageItems.length
        const total = result.total ?? received
        return {
          ...row,
          items: [...row.items, ...pageItems],
          nextPage: row.nextPage + 1,
          received,
          total,
          complete: pageItems.length === 0 || received >= total,
          error: '',
        }
      })
      storeRows(nextRows)
      setLoading(false)
    }
    loadingRef.current = false
  }, [storeRows])

  const loadLibraries = useCallback(async () => {
    if (loadingRef.current) return
    setCatalogError('')
    setLoading(true)
    try {
      const catalog = await libraryAPI.list()
      const rows = catalog.map(initialPosterLibraryState)
      cursorRef.current = 0
      storeRows(rows)
      await loadBatch(rows)
    } catch (error) {
      if (activeRef.current) {
        setCatalogError(
          (error as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '媒体库目录加载失败',
        )
        setLoading(false)
      }
    }
  }, [loadBatch, storeRows])

  useEffect(() => {
    activeRef.current = true
    void loadLibraries()
    return () => {
      activeRef.current = false
    }
  }, [loadLibraries])

  const items = useMemo(() => deduplicateMedia(libraries.flatMap((row) => row.items)), [libraries])
  const cards = useMemo(() => groupSeries(items), [items])
  const errors = libraries.filter((row) => row.error)
  const complete = libraries.length === 0 || libraries.every((row) => row.complete)

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3">
        <GalleryHorizontalEnd className="h-6 w-6 text-brand-500" />
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">海报视图</h1>
          <p className="text-sm text-ink-50">按剧集聚合 · 已加载 {cards.length} 个条目</p>
        </div>
      </header>

      {catalogError && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{catalogError}</span>
          <button type="button" onClick={() => void loadLibraries()} className="min-h-11 font-bold underline">重试</button>
        </div>
      )}
      {errors.length > 0 && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
          {errors.map((row) => `${row.library.name}：${row.error}`).join('；')}。再次加载会重试失败页。
        </div>
      )}
      {loading && cards.length === 0 && !catalogError && <p className="text-sand-500">加载中…</p>}
      {!loading && !catalogError && cards.length === 0 && complete && (
        <p className="text-ink-50">暂无媒体。请先添加媒体库并扫描。</p>
      )}
      <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10">
        {cards.map((card) => (
          <div key={card.key} className="relative">
            <MediaCard media={card.rep} linkTo={seriesCardLink(card)} />
            {card.count > 1 && (
              <span className="pointer-events-none absolute right-1.5 top-1.5 inline-flex items-center gap-0.5 rounded-lg bg-black/60 px-1.5 py-0.5 text-[10px] font-medium text-white backdrop-blur-sm">
                <Layers size={10} />
                {card.count}
              </span>
            )}
          </div>
        ))}
      </div>
      {!catalogError && !complete && (
        <div className="flex justify-center pt-2">
          <button
            type="button"
            onClick={() => void loadBatch()}
            disabled={loading}
            className="neon-button min-h-11 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {loading ? '正在加载…' : errors.length > 0 ? '重试并加载更多' : '加载更多'}
          </button>
        </div>
      )}
    </div>
  )
}

function initialPosterLibraryState(library: Library): PosterLibraryState {
  return {
    library,
    items: [],
    nextPage: 1,
    received: 0,
    total: null,
    complete: false,
    error: '',
  }
}

function selectPosterBatch(rows: PosterLibraryState[], cursor: number) {
  if (rows.length === 0) return { indexes: [] as number[], nextCursor: 0 }
  const incomplete = rows.map((row, index) => ({ row, index })).filter(({ row }) => !row.complete)
  if (incomplete.length === 0) return { indexes: [] as number[], nextCursor: cursor }
  const firstPages = incomplete.filter(({ row }) => row.nextPage === 1)
  const pool = firstPages.length > 0 ? firstPages : incomplete
  const ordered = [...pool].sort((left, right) => {
    const leftDistance = (left.index - cursor + rows.length) % rows.length
    const rightDistance = (right.index - cursor + rows.length) % rows.length
    return leftDistance - rightDistance
  })
  const indexes = ordered.slice(0, REQUESTS_PER_BATCH).map(({ index }) => index)
  return {
    indexes,
    nextCursor: indexes.length > 0 ? (indexes[indexes.length - 1] + 1) % rows.length : cursor,
  }
}

function deduplicateMedia(items: Media[]): Media[] {
  const seen = new Set<string>()
  return items.filter((item) => {
    if (seen.has(item.id)) return false
    seen.add(item.id)
    return true
  })
}
