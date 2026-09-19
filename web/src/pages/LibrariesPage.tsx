import { useEffect, useState } from 'react'
import { GalleryHorizontalEnd, LayoutGrid } from 'lucide-react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'

import { libraryAPI } from '../api/library'
import { useAuthStore } from '../stores/auth'
import type { Library } from '../types'
import { AdminLibraryPanel } from './AdminLibraryPanel'
import { AdminLibraryGrid } from './AdminLibraryTable'
import { PosterWallPage } from './PosterWallPage'

export function LibrariesPage() {
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const [searchParams] = useSearchParams()
  const viewValues = searchParams.getAll('view')
  const view = viewValues[0] ?? 'library'
  const validView = viewValues.length <= 1 && (view === 'library' || view === 'poster')

  if (!validView) return <Navigate to="/libraries" replace />

  return (
    <div className="space-y-8">
      <LibrariesViewSwitcher view={view} />
      {view === 'poster' ? (
        <PosterWallPage />
      ) : (
        <section className="space-y-5">
          {isAdmin ? <AdminLibraryPanel /> : <ViewerLibraryGrid />}
        </section>
      )}
    </div>
  )
}

function ViewerLibraryGrid() {
  const [libs, setLibs] = useState<Library[] | null>(null)

  useEffect(() => {
    let cancelled = false
    libraryAPI.list()
      .then((items) => { if (!cancelled) setLibs(items) })
      .catch(() => { if (!cancelled) setLibs([]) })
    return () => { cancelled = true }
  }, [])

  if (!libs) return <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
  return <AdminLibraryGrid libs={libs} />
}

function LibrariesViewSwitcher({ view }: { view: string }) {
  const items = [
    { id: 'library', label: '媒体库', icon: LayoutGrid, to: '/libraries' },
    { id: 'poster', label: '海报视图', icon: GalleryHorizontalEnd, to: '/libraries?view=poster' },
  ]
  return (
    <nav aria-label="媒体库视图" className="tab-list w-fit">
      {items.map((item) => {
        const Icon = item.icon
        const active = view === item.id
        return (
          <Link
            key={item.id}
            to={item.to}
            aria-current={active ? 'page' : undefined}
            className="tab-item"
          >
            <Icon size={16} />
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}
