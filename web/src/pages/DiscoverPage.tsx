import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { discoverAPI, type DiscoverItem, type DiscoverSection } from '../api/discover'
import { DiscoverSkeleton } from './DiscoverContentRow'
import { DiscoverDetailModal } from './DiscoverDetailModal'
import { useSearchParams } from 'react-router-dom'
import { DiscoverEmptySelection, DiscoverHeader, DiscoverResults } from './DiscoverPageSections'
import {
  defaultSections,
  discoverStorageKey,
  readCachedDiscoverRows,
  readSavedSections,
  serializeSavedSections,
  writeCachedDiscoverRow,
} from './discoverPageModel'

type Provider = 'tmdb' | 'douban' | 'bangumi'
export function DiscoverPage({ provider }: { provider: Provider }) {
  return <DiscoverFeed provider={provider} />
}

function DiscoverFeed({ provider }: { provider: Provider }) {
  const [params, setParams] = useSearchParams()
  const rank = params.get('rank') || ''
  const rankCount = params.getAll('rank').length
  const [sections, setSections] = useState<DiscoverSection[]>([])
  const selectedKey = useMemo(() => {
    const available = new Set(sections.map(s => s.key))
    const key = rankCount === 1 && available.has(rank) ? rank : readSavedSections(sections)[0] || defaultSections.find(key => available.has(key)) || sections[0]?.key
    return key || ''
  }, [sections, rank, rankCount])
  const selected = useMemo(() => selectedKey ? [selectedKey] : [], [selectedKey])
  const [rows, setRows] = useState<Record<string, DiscoverItem[]>>({})
  const [rowCanNext, setRowCanNext] = useState<Record<string, boolean>>({})
  const [rowLoading, setRowLoading] = useState<Record<string, boolean>>({})
  const [rowUpdatedAt, setRowUpdatedAt] = useState<Record<string, string>>({})
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})
  const [sectionsReady, setSectionsReady] = useState(false)
  const [sectionsError, setSectionsError] = useState(false)
  const [sectionsRetry, setSectionsRetry] = useState(0)
  const [loading, setLoading] = useState(false)
  const [activeItem, setActiveItem] = useState<DiscoverItem | null>(null)
  const rowPagesRef = useRef<Record<string, number>>({})
  const failedPagesRef = useRef<Record<string, number>>({})
  const activeFeedRef = useRef<AbortController | null>(null)

  useEffect(() => {
    let cancelled = false
    setSectionsReady(false)
    setSectionsError(false)
    discoverAPI
      .sections()
      .then((items) => {
        if (cancelled) return
        const filtered = items.filter(item => item.provider === provider)
        setSections(filtered)
        setSectionsReady(true)
      })
      .catch(() => {
        if (cancelled) return
        setSections([])
        setSectionsError(true)
        setSectionsReady(true)
      })
    return () => {
      cancelled = true
    }
  }, [provider, sectionsRetry])

  useEffect(() => {
    if (!sectionsReady || sectionsError) return
    const next = new URLSearchParams(params)
    next.delete('tmdb_query'); next.delete('tmdb_kind')
    if (selected[0]) next.set('rank', selected[0]); else next.delete('rank')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, setParams, selected, sectionsReady, sectionsError])

  const loadRows = useCallback((targets: Record<string, number>, refresh = false) => {
    const targetEntries = Object.entries(targets)
    if (targetEntries.length === 0) return null

    activeFeedRef.current?.abort()
    const controller = new AbortController()
    activeFeedRef.current = controller
    const targetKeys = targetEntries.map(([key]) => key)
    setLoading(true)
    setRowLoading(Object.fromEntries(targetKeys.map((key) => [key, true])))
    setRowErrors((current) => {
      const next = { ...current }
      for (const key of targetKeys) delete next[key]
      return next
    })

    const requests = new Map<number, string[]>()
    for (const [key, page] of targetEntries) {
      requests.set(page, [...(requests.get(page) ?? []), key])
    }
    const pending = Array.from(requests, async ([page, keys]) => {
      try {
        const feed = await discoverAPI.feed(keys, page, refresh, controller.signal)
        if (controller.signal.aborted || activeFeedRef.current !== controller) return
        if (page === 1) setRowUpdatedAt(current => {
          const next = { ...current }
          for (const key of keys) {
            if (feed.meta[key]?.updated_at) next[key] = feed.meta[key].updated_at!
            else if (!feed.meta[key]?.error && !feed.meta[key]?.warning) delete next[key]
          }
          return next
        })
        setRows((current) => {
          const next = { ...current }
          for (const key of keys) {
            const error = feed.meta[key]?.error
            const nextItems = feed.items[key] ?? []
            if (!(error && nextItems.length === 0 && (current[key]?.length ?? 0) > 0)) {
              next[key] = page === 1 ? nextItems : Array.from(new Map([...(current[key] ?? []), ...nextItems].map((item) => [`${item.source}:${item.tmdb_id ?? item.douban_id ?? item.bangumi_id ?? item.title}`, item])).values())
            }
          }
          return next
        })
        setRowCanNext((current) => {
          const next = { ...current }
          for (const key of keys) {
            const error = feed.meta[key]?.error
            const nextItems = feed.items[key] ?? []
            if (!(error && nextItems.length === 0 && key in current)) {
              next[key] = Boolean(feed.meta[key]?.has_next)
            }
          }
          return next
        })
        setRowErrors((current) => {
          let next = current
          for (const key of keys) {
            next = updateDiscoverRowError(next, key, feed.meta[key]?.error || feed.meta[key]?.warning)
          }
          return next
        })
        for (const key of keys) {
          if (!feed.meta[key]?.error && !feed.meta[key]?.warning) {
            rowPagesRef.current[key] = page
            delete failedPagesRef.current[key]
            writeCachedDiscoverRow(
              key,
              page,
              feed.items[key] ?? [],
              Boolean(feed.meta[key]?.has_next),
            )
          } else failedPagesRef.current[key] = page
        }
      } catch (err) {
        if (controller.signal.aborted || activeFeedRef.current !== controller) return
        const message = discoverRequestErrorMessage(err)
        for (const key of keys) failedPagesRef.current[key] = page
        setRows((current) => {
          const next = { ...current }
          for (const key of keys) {
            if ((current[key]?.length ?? 0) === 0) next[key] = []
          }
          return next
        })
        setRowCanNext((current) => {
          const next = { ...current }
          for (const key of keys) {
            if (!(key in current)) next[key] = false
          }
          return next
        })
        setRowErrors((current) => {
          const next = { ...current }
          for (const key of keys) next[key] = message
          return next
        })
      } finally {
        if (activeFeedRef.current === controller) {
          setRowLoading((current) => {
            const next = { ...current }
            for (const key of keys) next[key] = false
            return next
          })
        }
      }
    })
    void Promise.all(pending).then(() => {
      if (activeFeedRef.current !== controller) return
      activeFeedRef.current = null
      setLoading(false)
    })
    return controller
  }, [])

  useEffect(
    () => () => {
      activeFeedRef.current?.abort()
      activeFeedRef.current = null
    },
    [],
  )

  useEffect(() => {
    if (!sectionsReady) return
    if (selected.length === 0) {
      activeFeedRef.current?.abort()
      activeFeedRef.current = null
      setRows({})
      setRowLoading({})
      setRowCanNext({})
      setRowErrors({})
      setLoading(false)
      return
    }
    setRowErrors({})
    setRows((current) => {
      const next: Record<string, DiscoverItem[]> = {}
      for (const key of selected) {
        next[key] = current[key] ?? []
      }
      return next
    })
    window.localStorage.setItem(discoverStorageKey, serializeSavedSections(selected))
    const targets = Object.fromEntries(selected.map(key => [key, 1]))
    rowPagesRef.current = {}
    failedPagesRef.current = {}
    const cached = readCachedDiscoverRows(selected)
    setRows(cached.rows)
    setRowCanNext(cached.rowCanNext)
    const controller = loadRows(targets)
    return () => {
      controller?.abort()
      if (activeFeedRef.current === controller) activeFeedRef.current = null
    }
  }, [loadRows, sections, sectionsReady, selected])

  const sectionMap = useMemo(
    () => new Map(sections.map((section) => [section.key, section])),
    [sections],
  )
  const hasContent = selected.some((key) => (rows[key] ?? []).length > 0)
  const sectionLabel = (key: string) => sectionMap.get(key)?.label ?? key

  const selectSection = (key: string) => {
    if (selected[0] === key) return
    const next = new URLSearchParams(params); next.set('rank', key); setParams(next)
  }

  const loadMore = (key: string) => {
    const currentPage = rowPagesRef.current[key] ?? 1
    if (rowLoading[key] || rowErrors[key] || !rowCanNext[key] || activeFeedRef.current) return
    const nextPage = currentPage + 1
    loadRows({ [key]: nextPage })
  }

  const refreshDiscover = () => {
    const key = selected[0]
    if (!key || activeFeedRef.current) return
    const nextPages = { [key]: 1 }
    loadRows(nextPages, true)
  }

  return (
    <div className="space-y-8 py-6">
      <DiscoverHeader
        updatedAt={rowUpdatedAt[selectedKey]}
        sections={sections}
        selected={selected}
        sectionsReady={sectionsReady}
        loading={loading}
        onRefresh={refreshDiscover}
        onSelectSection={selectSection}
      />

      {!sectionsReady && <DiscoverSkeleton />}

      {sectionsError && <div role="alert" className="space-y-3 text-sm text-ink-100"><p>榜单列表读取失败。</p><button type="button" className="btn-outline" onClick={() => setSectionsRetry(value => value + 1)}>重新加载</button></div>}

      {sectionsReady && !sectionsError && !loading && selected.length === 0 && (
        <DiscoverEmptySelection />
      )}

      {sectionsReady && selected.length > 0 && (
        <DiscoverResults
          selected={selected}
          rows={rows}
          rowLoading={rowLoading}
          rowErrors={rowErrors}
          rowCanNext={rowCanNext}
          loading={loading}
          hasContent={hasContent}
          sectionLabel={sectionLabel}
          onLoadMore={loadMore}
          onRetry={(key) => { if (!activeFeedRef.current) loadRows({ [key]: failedPagesRef.current[key] ?? 1 }, true) }}
          onSelect={setActiveItem}
        />
      )}

      {activeItem && (
        <DiscoverDetailModal
          item={activeItem}
          onClose={() => setActiveItem(null)}
        />
      )}
    </div>
  )
}

function updateDiscoverRowError(
  current: Record<string, string>,
  key: string,
  error?: string,
): Record<string, string> {
  if (error) return { ...current, [key]: error }
  if (!(key in current)) return current
  const next = { ...current }
  delete next[key]
  return next
}

function discoverRequestErrorMessage(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err)
  const lower = raw.toLowerCase()
  if (lower.includes('timeout') || lower.includes('deadline')) {
    return '推荐源请求超时，已跳过本次加载'
  }
  if (lower.includes('network')) {
    return '推荐源网络不可用，已跳过本次加载'
  }
  return '推荐源暂时不可用，已跳过本次加载'
}
