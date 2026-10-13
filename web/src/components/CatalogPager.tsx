export function CatalogPager({ page, total, size = 30, disabled = false, setPage }: { page: number; total: number; size?: number; disabled?: boolean; setPage: (page: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / size))
  return <footer className="flex flex-wrap items-center justify-between gap-3 text-sm text-ink-50"><span>共 {total} 条 · 第 {page} / {pages} 页</span>{(total > 0 || page > 1) && <div className="flex gap-2"><button type="button" className="btn-outline" disabled={disabled || page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button type="button" className="btn-outline" disabled={disabled || page >= pages} onClick={() => setPage(page + 1)}>下一页</button></div>}</footer>
}
