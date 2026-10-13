import type { ReactNode } from 'react'
import { RefreshCw, Search } from 'lucide-react'

export function CatalogSearchForm({ label, placeholder, value, maxLength = 200, loading, refreshLabel, onChange, onSearch, onRefresh, children }: {
  label: string; placeholder: string; value: string; maxLength?: number; loading: boolean; refreshLabel: string
  onChange: (value: string) => void; onSearch: () => void; onRefresh: () => void; children?: ReactNode
}) {
  return <form className="flex flex-wrap items-center gap-2" onSubmit={event => { event.preventDefault(); onSearch() }}>
    <label className="relative min-w-0 flex-1 basis-56"><Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-50" /><input type="search" autoComplete="off" className="input-field w-full pl-10" aria-label={label} placeholder={placeholder} maxLength={maxLength} value={value} onChange={event => onChange(event.target.value)} /></label>
    <button type="submit" className="btn-primary">搜索</button>
    <button type="button" className="btn-outline px-3" aria-label={refreshLabel} title={refreshLabel} disabled={loading} onClick={onRefresh}><RefreshCw size={16} aria-hidden="true" /></button>
    {children}
  </form>
}
