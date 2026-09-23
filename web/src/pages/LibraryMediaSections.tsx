import { Film } from 'lucide-react'

import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'
import type { SeriesCard } from '../utils/groupSeries'

type LibraryMediaSectionsProps = {
  isSeries: boolean
  items: Media[]
  seriesCards: SeriesCard[]
  selectedSeries: SeriesCard | null
  loading: boolean
  filtered: boolean
  detailFrom: string
  favouriteIds: ReadonlySet<string>
  onToggleFavourite: (media: Media) => void
  onSeriesClick: (series: SeriesCard) => void
}

// 网格密度：少列数 + 大间距，卡片更大更透气（参考 Netflix/Emby 海报网格）
const gridClass = 'grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 2xl:grid-cols-6'

export function LibraryMediaSections({
  isSeries,
  items,
  seriesCards,
  selectedSeries,
  loading,
  filtered,
  detailFrom,
  favouriteIds,
  onToggleFavourite,
  onSeriesClick,
}: LibraryMediaSectionsProps) {
  return (
    <>
      {!isSeries && items.length > 0 && (
        <div className={gridClass}>
          {items.map((media, index) => (
            <MediaCard
              key={media.metadata_id || media.id}
              media={media}
              linkTo={`/media/${media.metadata_id || media.id}`}
              favourite={favouriteIds.has(media.metadata_id || media.id)}
              onToggleFavourite={() => onToggleFavourite(media)}
              linkState={{ from: detailFrom }}
              staggerIndex={index}
            />
          ))}
        </div>
      )}

      {!isSeries && items.length === 0 && (
        <LibraryEmptyState filtered={filtered} message={filtered ? '没有符合当前筛选条件的媒体' : '该媒体库暂无内容，触发一次扫描后再来看看'} />
      )}

      {isSeries && seriesCards.length > 0 && !selectedSeries && (
        <div className={gridClass}>
          {seriesCards.map((series, index) => (
            <MediaCard
              key={series.key}
              media={series.rep}
              count={series.rep.catalog_source === 'hongguo' && !series.rep.series_id ? undefined : series.count}
              onClick={series.rep.catalog_source === 'hongguo' && !series.rep.series_id ? undefined : () => onSeriesClick(series)}
              linkTo={series.rep.catalog_source === 'hongguo' && !series.rep.series_id ? `/media/${series.rep.id}` : undefined}
              linkState={{ from: detailFrom }}
              staggerIndex={index}
            />
          ))}
        </div>
      )}

      {isSeries && seriesCards.length === 0 && !loading && (
        <LibraryEmptyState filtered={filtered} message={filtered ? '没有符合当前筛选条件的剧集' : '该库尚未发现任何剧集，触发一次扫描后再来看看'} />
      )}
    </>
  )
}

function LibraryEmptyState({ message, filtered }: { message: string; filtered: boolean }) {
  return (
    <div className="relative flex flex-col items-center justify-center overflow-hidden rounded-3xl border border-[var(--app-border)] bg-[var(--app-panel)] py-24 text-center">
      <div aria-hidden="true" className="pointer-events-none absolute -top-16 left-1/2 h-40 w-80 -translate-x-1/2 rounded-full bg-brand-500/10 blur-3xl" />
      <div className="relative mb-5 flex h-20 w-20 items-center justify-center rounded-3xl border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] text-[var(--app-accent)] shadow-glow-sm">
        <Film size={30} className="stroke-[1.5]" />
      </div>
      <p className="relative font-display text-lg font-extrabold tracking-tight text-[var(--app-text)]">{message}</p>
      <p className="relative mt-2 text-sm text-[var(--app-muted)]">
        {filtered ? '请调整或关闭筛选条件' : '扫描完成后，海报墙会自动出现在这里'}
      </p>
    </div>
  )
}
