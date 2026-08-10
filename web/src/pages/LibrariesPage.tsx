import { useEffect, useMemo, useState } from 'react'
import { GalleryHorizontalEnd, LayoutGrid } from 'lucide-react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import clsx from 'clsx'

import { libraryAPI } from '../api/library'
import { toolsAPI } from '../api/tools'
import { useAuthStore } from '../stores/auth'
import {
  LibrariesContent,
  LibrariesEmptyState,
  LibrariesHeader,
} from './LibrariesPageSections'
import { isSeriesLibraryType, latestLibraryCards, type LibraryPreview } from './librariesPageModel'
import { PosterWallPage } from './PosterWallPage'

export function LibrariesPage() {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const [searchParams] = useSearchParams()
  const viewValues = searchParams.getAll('view')
  const view = viewValues[0] ?? 'library'
  const validView = viewValues.length <= 1 && (view === 'library' || view === 'poster')
  const [previews, setPreviews] = useState<LibraryPreview[]>([])
  const [loading, setLoading] = useState(true)
  const [repairing, setRepairing] = useState(false)
  const [repairEpisodeArtwork, setRepairEpisodeArtwork] = useState(false)
  const [repairMsg, setRepairMsg] = useState('')

  async function handleRepairRescrape() {
    if (repairing) return
    setRepairing(true)
    setRepairMsg('')
    try {
      await toolsAPI.repairAndRescrapeAll({ episode_images: repairEpisodeArtwork, refresh_matched: true })
      setRepairMsg('已开始全库修复+重刮，进度可在任务中查看。')
    } catch {
      setRepairMsg('启动失败，请稍后重试。')
    } finally {
      setRepairing(false)
    }
  }

  useEffect(() => {
    if (!validView || view !== 'library') return undefined
    let cancelled = false
    async function load() {
      setLoading(true)
      try {
        const libs = await libraryAPI.list()
        const rows = await Promise.all(libs.map(async (library) => {
          try {
            if (isSeriesLibraryType(library.type)) {
              const [seriesPage, mediaPage] = await Promise.all([
                libraryAPI.listSeries(library.id, 1, 10),
                libraryAPI.listMedia(library.id, 1, 1, { groupVersions: false }),
              ])
              return { library, items: [], total: mediaPage.total, cards: seriesPage.items ?? [] } satisfies LibraryPreview
            }
            const page = await libraryAPI.listMedia(library.id, 1, 160, { groupVersions: false })
            const cards = latestLibraryCards(page.items)
            return { library, items: page.items, total: page.total, cards } satisfies LibraryPreview
          } catch {
            return { library, items: [], total: 0, cards: [] } satisfies LibraryPreview
          }
        }))
        if (!cancelled) setPreviews(rows)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [validView, view])

  const total = useMemo(() => previews.reduce((sum, preview) => sum + preview.total, 0), [previews])

  if (!validView) return <Navigate to="/libraries" replace />

  return (
    <div className="space-y-8">
      <LibrariesViewSwitcher view={view} />
      {view === 'poster' ? (
        <PosterWallPage />
      ) : (
        <>
          {loading ? (
            <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
          ) : (
            <>
              <LibrariesHeader
                isAdmin={isAdmin}
                previewCount={previews.length}
                total={total}
                repairMsg={repairMsg}
                repairEpisodeArtwork={repairEpisodeArtwork}
                repairing={repairing}
                onRepairEpisodeArtworkChange={setRepairEpisodeArtwork}
                onRepairRescrape={handleRepairRescrape}
              />
              {previews.length === 0 ? <LibrariesEmptyState /> : <LibrariesContent previews={previews} />}
            </>
          )}
        </>
      )}
    </div>
  )
}

function LibrariesViewSwitcher({ view }: { view: string }) {
  const items = [
    { id: 'library', label: '媒体库', icon: LayoutGrid, to: '/libraries' },
    { id: 'poster', label: '海报视图', icon: GalleryHorizontalEnd, to: '/libraries?view=poster' },
  ]
  return (
    <nav aria-label="媒体库视图" className="inline-grid grid-cols-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)] p-1">
      {items.map((item) => {
        const Icon = item.icon
        const active = view === item.id
        return (
          <Link
            key={item.id}
            to={item.to}
            aria-current={active ? 'page' : undefined}
            className={clsx(
              'flex min-h-11 items-center justify-center gap-2 rounded-md px-4 text-sm font-bold transition-colors',
              active
                ? 'bg-[var(--app-active-bg)] text-[var(--app-active-text)]'
                : 'text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)]',
            )}
          >
            <Icon size={16} />
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}
