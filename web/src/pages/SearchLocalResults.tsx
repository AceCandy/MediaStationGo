import { MediaCard } from '../components/MediaCard'
import type { SeriesCard } from '../utils/groupSeries'
import { seriesCardLink } from '../utils/groupSeries'

type SearchLocalResultsProps = {
  localCards: SeriesCard[]
  itemCount: number
  searchTotal: number
  loading: boolean
  hasMore: boolean
  onLoadMore: () => void
}

export function SearchLocalResults({
  localCards,
  itemCount,
  searchTotal,
  loading,
  hasMore,
  onLoadMore,
}: SearchLocalResultsProps) {
  if (localCards.length === 0 && !hasMore) return null

  return (
    <>
      <div className="text-sm font-semibold text-ink-100">
        本地媒体库 · {localCards.length} 个合集 / {itemCount} 个条目
        {searchTotal > itemCount ? ` · 已加载 ${itemCount}/${searchTotal}` : ''}
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {localCards.map((card, index) => (
          <MediaCard
            key={card.key}
            media={card.rep}
            count={card.count}
            linkTo={seriesCardLink(card)}
            staggerIndex={index}
          />
        ))}
      </div>
      {hasMore && (
        <button type="button" className="btn-secondary" disabled={loading} onClick={onLoadMore}>
          {loading ? '加载中…' : '加载更多'}
        </button>
      )}
    </>
  )
}
