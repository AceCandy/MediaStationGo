import { AlertTriangle } from 'lucide-react'

import type { DiscoverItem, DiscoverSection } from '../api/discover'
import { CatalogRankingHeader, CatalogRankingSkeleton } from '../components/CatalogRanking'
import { ContentRow } from './DiscoverContentRow'

type SectionLabel = (key: string) => string

export function DiscoverHeader({
  sections,
  selected,
  sectionsReady,
  loading,
  onRefresh,
  updatedAt,
  onSelectSection,
}: {
  sections: DiscoverSection[]
  selected: string[]
  sectionsReady: boolean
  loading: boolean
  updatedAt?: string
  onRefresh: () => void
  onSelectSection: (key: string) => void
}) {
  return (
    <header className="space-y-3">
      <CatalogRankingHeader source={sections.find(s => s.key === selected[0])?.provider || ''} title={sections.find(s => s.key === selected[0])?.label || '榜单'} options={sections.map(s => ({value:s.key, label:s.label}))} value={selected[0] || ''} onChange={onSelectSection} onRefresh={onRefresh} refreshing={loading} refreshDisabled={!sectionsReady || selected.length === 0} updatedAt={updatedAt} />
    </header>
  )
}

export function DiscoverEmptySelection() {
  return (
    <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center text-sand-500">
      至少选择一个推荐源，小宇宙才会开始转动。
    </div>
  )
}

export function DiscoverResults({
  selected,
  rows,
  rowLoading,
  rowErrors,
  rowCanNext,
  loading,
  hasContent,
  sectionLabel,
  onLoadMore,
  onRetry,
  onSelect,
}: {
  selected: string[]
  rows: Record<string, DiscoverItem[]>
  rowLoading: Record<string, boolean>
  rowErrors: Record<string, string>
  rowCanNext: Record<string, boolean>
  loading: boolean
  hasContent: boolean
  sectionLabel: SectionLabel
  onLoadMore: (key: string) => void
  onRetry: (key: string) => void
  onSelect: (item: DiscoverItem) => void
}) {
  const hasRowErrors = Object.keys(rowErrors).length > 0

  return (
    <div className="space-y-10">
      {selected.map((key) => {
        const items = rows[key] ?? []
        if (items.length === 0) {
          if (rowLoading[key]) {
            return <CatalogRankingSkeleton key={key} />
          }
          return null
        }
        return (
          <ContentRow
            key={key}
            items={items}
            rankLabel={sectionLabel(key)}
            canNext={Boolean(rowCanNext[key]) && !rowErrors[key]}
            loading={Boolean(rowLoading[key])}
            failed={Boolean(rowErrors[key])}
            onLoadMore={() => onLoadMore(key)}
            onSelect={onSelect}
          />
        )
      })}

      {hasRowErrors && (
        <DiscoverRowErrors rowErrors={rowErrors} sectionLabel={sectionLabel} onRetry={onRetry} />
      )}

      {!loading && !hasContent && !hasRowErrors && <DiscoverNoContent />}
    </div>
  )
}

function DiscoverRowErrors({
  rowErrors,
  sectionLabel,
  onRetry,
}: {
  rowErrors: Record<string, string>
  sectionLabel: SectionLabel
  onRetry: (key: string) => void
}) {
  return (
    <div role="alert" className="flex items-start gap-3 rounded-lg border border-amber-300/70 bg-amber-50 px-3 py-2 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100">
      <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-amber-500" />
      <div className="space-y-1 text-xs">
        <p className="font-semibold">部分推荐源暂不可用，其他已加载内容不受影响。</p>
        {Object.entries(rowErrors).map(([key, message]) => (
          <p key={key}>{sectionLabel(key)}：{message} <button type="button" className="underline" onClick={() => onRetry(key)}>重试</button></p>
        ))}
      </div>
    </div>
  )
}

function DiscoverNoContent() {
  return (
    <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center">
      <p className="text-sand-500">
        当前选择的推荐源暂未返回内容，可切换豆瓣 / Bangumi 或检查网络代理。
      </p>
    </div>
  )
}
