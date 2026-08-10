import { Clock, Heart, ListMusic } from 'lucide-react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import clsx from 'clsx'

import { FavouritesPage } from './FavouritesPage'
import { PlaylistsPage } from './PlaylistsPage'
import { WatchHistoryPage } from './WatchHistoryPage'

const TABS = [
  { id: 'favourites', label: '收藏', icon: Heart },
  { id: 'playlists', label: '播放列表', icon: ListMusic },
  { id: 'history', label: '观看历史', icon: Clock },
] as const

type MeTab = (typeof TABS)[number]['id']

export function MePage() {
  const [searchParams] = useSearchParams()
  const tabValues = searchParams.getAll('tab')
  const requestedTab = tabValues[0]
  const activeTab = (requestedTab ?? 'favourites') as MeTab
  const valid = tabValues.length <= 1 && TABS.some((tab) => tab.id === activeTab)

  if (!valid) return <Navigate to="/me?tab=favourites" replace />

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-3xl font-bold text-[var(--app-text)]">我的</h1>
        <p className="mt-1 text-sm text-[var(--app-muted)]">收藏内容、播放列表与观看记录</p>
      </div>

      <nav aria-label="我的内容" className="flex min-w-0 gap-1 overflow-x-auto border-b border-[var(--app-border)]">
        {TABS.map((tab) => {
          const Icon = tab.icon
          const active = tab.id === activeTab
          return (
            <Link
              key={tab.id}
              to={`/me?tab=${tab.id}`}
              aria-current={active ? 'page' : undefined}
              className={clsx(
                'flex min-h-11 shrink-0 items-center gap-2 border-b-2 px-4 text-sm font-bold transition-colors',
                active
                  ? 'border-brand-500 text-[var(--app-active-text)]'
                  : 'border-transparent text-[var(--app-muted)] hover:text-[var(--app-text)]',
              )}
            >
              <Icon size={16} />
              {tab.label}
            </Link>
          )
        })}
      </nav>

      {activeTab === 'favourites' && <FavouritesPage embedded />}
      {activeTab === 'playlists' && <PlaylistsPage embedded />}
      {activeTab === 'history' && <WatchHistoryPage embedded />}
    </div>
  )
}
