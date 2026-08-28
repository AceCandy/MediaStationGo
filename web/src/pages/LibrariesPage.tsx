import { useEffect, useMemo, useState } from 'react'
import { GalleryHorizontalEnd, LayoutGrid } from 'lucide-react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import clsx from 'clsx'

import { libraryAPI } from '../api/library'
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
  const [libraryCount, setLibraryCount] = useState(0)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!validView || view !== 'library') return undefined
    let cancelled = false
    async function load() {
      setLoading(true)
      setPreviews([])
      setLibraryCount(0)
      try {
        const libs = await libraryAPI.list()
        if (cancelled) return
        setLibraryCount(libs.length)
        const previewByID = new Map<string, LibraryPreview>()
        const publish = () => {
          if (cancelled) return
          setPreviews(libs.map((library) => previewByID.get(library.id)).filter((preview): preview is LibraryPreview => Boolean(preview)))
        }
        await Promise.all(libs.map(async (library) => {
          let preview: LibraryPreview
          try {
            if (isSeriesLibraryType(library.type)) {
              const [seriesPage, mediaPage] = await Promise.all([
                libraryAPI.listSeries(library.id, 1, 10),
                libraryAPI.listMedia(library.id, 1, 1, { groupVersions: false }),
              ])
              preview = { library, items: [], total: mediaPage.total, cards: seriesPage.items ?? [] }
            } else {
              const page = await libraryAPI.listMedia(library.id, 1, 160, { groupVersions: false })
              preview = { library, items: page.items, total: page.total, cards: latestLibraryCards(page.items) }
            }
          } catch {
            preview = { library, items: [], total: 0, cards: [] }
          }
          previewByID.set(library.id, preview)
          publish()
        }))
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
          {loading && previews.length === 0 ? (
            <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
          ) : (
            <>
              <LibrariesHeader
                isAdmin={isAdmin}
                previewCount={libraryCount}
                total={total}
              />
              {previews.length === 0 && !loading ? <LibrariesEmptyState /> : previews.length > 0 && <LibrariesContent previews={previews} />}
              {loading && <p className="px-2 text-sm text-sand-500">媒体库内容加载中…</p>}
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
