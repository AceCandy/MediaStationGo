export function CatalogLoadMore({ loading, hasMore, hasItems, failed = false, onLoadMore, endLabel = '榜单已加载完' }: {
  loading: boolean; hasMore: boolean; hasItems: boolean; failed?: boolean; onLoadMore: () => void; endLabel?: string
}) {
  if (!hasItems) return null
  if (loading) return <p role="status" className="py-3 text-center text-sm text-ink-50">加载更多…</p>
  if (failed) return null
  return hasMore ? <div className="flex justify-center"><button type="button" className="btn-outline" onClick={onLoadMore}>加载更多</button></div> : <p role="status" className="py-3 text-center text-xs text-ink-50">{endLabel}</p>
}
