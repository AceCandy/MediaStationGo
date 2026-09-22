import { Navigate } from 'react-router-dom'

import { ExternalResults } from './SearchExternalResults'
import { SearchLocalResults } from './SearchLocalResults'
import { SearchStatusPanels } from './SearchStatusPanels'
import { useSearchPage } from './useSearchPage'

export function SearchPage() {
  const search = useSearchPage()

  if (search.normalizationTarget) return <Navigate to={search.normalizationTarget} replace />

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-ink-600">搜索</h1>

      <SearchStatusPanels
        loading={search.loading}
        error={search.error}
        showIdle={search.showIdle}
        showEmpty={search.showEmpty}
      />

      {search.externalItems.length > 0 && (
        <ExternalResults
          items={search.externalItems}
        />
      )}

      <SearchLocalResults
        localCards={search.localCards}
        itemCount={search.itemCount}
        searchTotal={search.searchTotal}
        loading={search.loading}
        hasMore={search.hasMore}
        onLoadMore={search.loadMore}
      />
    </div>
  )
}
