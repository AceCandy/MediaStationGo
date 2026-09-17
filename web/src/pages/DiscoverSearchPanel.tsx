import { useEffect, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Search } from 'lucide-react'
import { discoverAPI, discoverIdentityKey, discoverTMDbIdentity, type DiscoverItem } from '../api/discover'
import { Select } from '../components/Select'
import { ContentRow, DiscoverSkeleton } from './DiscoverContentRow'
import { DiscoverDetailModal } from './DiscoverDetailModal'

export function DiscoverSearchPanel({ children }: { children: ReactNode }) {
  const [params, setParams] = useSearchParams()
  const keyword = (params.get('tmdb_query') ?? '').trim().slice(0, 100)
  const rawKind = params.get('tmdb_kind') ?? 'multi'
  const kind = rawKind === 'movie' || rawKind === 'tv' ? rawKind : 'multi'
  const [query, setQuery] = useState(keyword)
  const [revision, setRevision] = useState(0)
  useEffect(() => { setQuery(keyword) }, [keyword])
  useEffect(() => {
    const next = new URLSearchParams(params)
    if (keyword) next.set('tmdb_query', keyword); else next.delete('tmdb_query')
    if (kind !== 'multi') next.set('tmdb_kind', kind); else next.delete('tmdb_kind')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, setParams, keyword, kind])
  return <div className="space-y-6">
    <form className="flex flex-wrap gap-2 pt-6" onSubmit={(event) => {
      event.preventDefault()
      const next = new URLSearchParams(params)
      if (query.trim()) next.set('tmdb_query', query.trim()); else next.delete('tmdb_query')
      setParams(next)
      setRevision((value) => value + 1)
    }}>
      <label className="relative min-w-0 flex-1 sm:max-w-sm"><Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-50" /><input type="search" maxLength={100} className="input-field w-full pl-10" aria-label="搜索 TMDb 作品" placeholder="搜索 TMDb 电影或电视剧" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
      <Select aria-label="搜索作品类型" className="input-field w-28" value={kind} onChange={(value) => { const next = new URLSearchParams(params); next.set('tmdb_kind', value); setParams(next) }}><option value="multi">全部</option><option value="movie">电影</option><option value="tv">电视剧</option></Select>
      <button className="btn-primary">搜索</button>
      {keyword && <button type="button" className="btn-outline" onClick={() => { const next = new URLSearchParams(params); next.delete('tmdb_query'); setParams(next) }}>返回榜单</button>}
    </form>
    {keyword ? <SearchResults key={`${keyword}:${kind}:${revision}`} keyword={keyword} kind={kind} /> : children}
  </div>
}

function SearchResults({ keyword, kind }: { keyword: string; kind: string }) {
  const [items, setItems] = useState<DiscoverItem[]>([])
  const [page, setPage] = useState(1)
  const [revision, setRevision] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [hasNext, setHasNext] = useState(false)
  const [active, setActive] = useState<DiscoverItem | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError(false)
    void discoverAPI.search(keyword, kind, page, controller.signal).then((result) => {
      if (controller.signal.aborted) return
      setItems((current) => Array.from(new Map([...current, ...result.items].map((item) => {
        const identity = discoverTMDbIdentity(item)!
        return [discoverIdentityKey(identity), item]
      })).values()))
      setHasNext(result.has_next)
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [keyword, kind, page, revision])
  return <section className="space-y-4">
    <p className="text-sm text-ink-50">TMDb 搜索 · 已显示 {items.length} 项 · 缺失资料将在后台自动补齐</p>
    {items.length > 0 && <ContentRow items={items} canNext={hasNext && !error} loading={loading} onSelect={setActive} onLoadMore={() => { if (!loading && !error) { setLoading(true); setPage((value) => value + 1) } }} />}
    {loading && items.length === 0 && <DiscoverSkeleton />}
    {error && <p role="alert">搜索或资料登记失败。<button type="button" className="btn-outline ml-2" onClick={() => setRevision((value) => value + 1)}>重试</button></p>}
    {!loading && !error && items.length === 0 && <p className="text-ink-50">暂无匹配作品</p>}
    {!loading && !error && items.length === 0 && hasNext && <button type="button" className="btn-outline" onClick={() => setPage((value) => value + 1)}>继续加载</button>}
    {active && <DiscoverDetailModal item={active} onClose={() => setActive(null)} />}
  </section>
}
