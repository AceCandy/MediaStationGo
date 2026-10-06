import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Film, RefreshCw, Search } from 'lucide-react'
import { huangguoaiAPI, huangGuoCategories, huangGuoRanks, type HuangGuoAIWork } from '../api/huangguoai'
import { imageURL } from '../api/client'
import { HuangGuoAIDetailModal } from './HuangGuoAIDetailModal'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'
import { CatalogRankingHeader, CatalogRankingRow } from '../components/CatalogRanking'

export function HuangGuoAIPage() {
  const [params, setParams] = useSearchParams()
  const accessKey = useMediaAccessKey()
  const category = huangGuoCategories.some(([key]) => key === params.get('category')) ? params.get('category')! : ''
  const rank = huangGuoRanks.some(([key]) => key === params.get('rank')) ? params.get('rank')! : ''
  const section = rank || params.get('section') === 'rank' ? 'rank' : 'category'
  const keyword = (params.get('keyword') ?? '').slice(0, 200)
  const rawPage = Number(params.get('page') ?? 1)
  const page = !keyword && section !== 'rank' && Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 10000 ? rawPage : 1
  const id = /^\d+$/.test(params.get('id') ?? '') ? params.get('id')! : ''
  useEffect(() => {
    const next = new URLSearchParams(params)
    const values = { category: section === 'rank' ? '' : category, rank: section === 'rank' ? rank || 'hot' : '', section: section === 'rank' ? 'rank' : '', keyword, tag: '', mode: '', page: String(page), id }
    for (const [key, value] of Object.entries(values)) { if (value) next.set(key, value); else next.delete(key) }
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, setParams, category, rank, keyword, page, id, section])
  const navigate = (changes: Record<string, string>) => { const next = new URLSearchParams(params); for (const [key, value] of Object.entries(changes)) { if (value) next.set(key, value); else next.delete(key) }; setParams(next) }
  return <HuangGuoAIContent key={`${accessKey}:${category}:${rank}:${keyword}:${section}:${page}`} {...{ category: section === 'category' ? category : '', rank: section === 'rank' ? rank || 'hot' : '', section, keyword, page, id, navigate }} />
}

function HuangGuoAIContent({ category, rank, section, keyword, page, id, navigate }: { section: 'category' | 'rank'; category: string; rank: string; keyword: string; page: number; id: string; navigate: (changes: Record<string, string>) => void }) {
  const ranking = section === 'rank' && !keyword
  const pageSize = ranking ? 10 : 50
  const [rows, setRows] = useState<HuangGuoAIWork[]>([])
  const [total, setTotal] = useState(0)
  const [localHasMore, setLocalHasMore] = useState(false)
  const [localLoading, setLocalLoading] = useState(true)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [query, setQuery] = useState(keyword)
  const [catalogPage, setCatalogPage] = useState(page)
  const loadMoreRef = useRef<HTMLDivElement>(null)
  const [remoteRows, setRemoteRows] = useState<HuangGuoAIWork[]>([])
  const [remotePage, setRemotePage] = useState(1)
  const [remoteHasMore, setRemoteHasMore] = useState(false)
  const [remoteLoading, setRemoteLoading] = useState(Boolean(keyword))
  const [remoteError, setRemoteError] = useState(false)
  const [remoteRetry, setRemoteRetry] = useState(0)
  const loading = localLoading || remoteLoading
  const hasMore = localHasMore || remoteHasMore
  const canLoadMore = (localHasMore && !error && catalogPage < 10000) || (remoteHasMore && !remoteError && remotePage < 10000)
  const visibleRows = mergeWorks(remoteRows, rows)
  const refresh = () => {
    setRows([]); setRemoteRows([]); setLocalHasMore(false); setRemoteHasMore(false)
    setLocalLoading(true); setRemoteLoading(Boolean(keyword)); setCatalogPage(page); setRemotePage(1)
    setRetry(v => v + 1); setRemoteRetry(v => v + 1)
  }
  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      try {
        const data = await huangguoaiAPI.list({ keyword, category: keyword ? '' : category, tag: '', rank: keyword ? '' : rank, page: catalogPage, page_size: pageSize }, controller.signal)
        if (!controller.signal.aborted) {
          setRows(current => mergeWorks(catalogPage === page ? [] : current, data.items))
          setTotal(data.total)
          setLocalHasMore(catalogPage * pageSize < data.total && (!ranking || data.items.length === pageSize))
        }
      } catch { if (!controller.signal.aborted) setError(true) }
      finally { if (!controller.signal.aborted) setLocalLoading(false) }
    }
    setLocalLoading(true); setError(false); void load()
    return () => controller.abort()
  }, [keyword, category, rank, page, catalogPage, retry, pageSize, ranking])
  useEffect(() => {
    if (!keyword) return
    const controller = new AbortController()
    setRemoteLoading(true); setRemoteError(false)
    void huangguoaiAPI.search(keyword, remotePage, controller.signal).then(data => {
      if (!controller.signal.aborted) {
        setRemoteRows(current => mergeWorks(remotePage === 1 ? [] : current, data.items))
        setRemoteHasMore(data.has_more)
      }
    }).catch(() => { if (!controller.signal.aborted) setRemoteError(true) })
      .finally(() => { if (!controller.signal.aborted) setRemoteLoading(false) })
    return () => controller.abort()
  }, [keyword, remotePage, remoteRetry])
  const loadMore = () => {
    if (localHasMore && !localLoading && !error && catalogPage < 10000) { setLocalLoading(true); setCatalogPage(v => v + 1) }
    if (remoteHasMore && !remoteLoading && !remoteError && remotePage < 10000) { setRemoteLoading(true); setRemotePage(v => v + 1) }
  }
  useEffect(() => {
    const target = loadMoreRef.current
    if (!target || id || loading || !canLoadMore) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) {
        observer.disconnect()
        if (localHasMore && !error && catalogPage < 10000) { setLocalLoading(true); setCatalogPage(v => v + 1) }
        if (remoteHasMore && !remoteError && remotePage < 10000) { setRemoteLoading(true); setRemotePage(v => v + 1) }
      }
    }, { rootMargin: ranking ? '0px' : '320px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [id, loading, error, remoteError, hasMore, catalogPage, remotePage, localHasMore, remoteHasMore, ranking, canLoadMore])
  return <section className="space-y-6">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <nav className="flex gap-2" aria-label="黄果AI发现模式">
        <button className={section === 'category' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'category'} onClick={() => navigate({ section: '', rank: '', keyword: '', page: '1', id: '' })}>分类</button>
        <button className={section === 'rank' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'rank'} onClick={() => navigate({ section: 'rank', rank: 'hot', category: '', keyword: '', page: '1', id: '' })}>榜单</button>
      </nav>
      <form className="flex w-full flex-wrap gap-2 sm:w-auto" onSubmit={event => { event.preventDefault(); if (query.trim() === keyword) { refresh(); return } navigate({ keyword: query.trim(), category: '', rank: '', section: '', page: '1', id: '' }) }}>
        <label className="relative w-full min-w-0 sm:w-auto sm:flex-1"><Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-50" /><input className="input-field w-full pl-10 sm:w-64" type="search" maxLength={200} aria-label="搜索黄果AI标题或ID" placeholder="搜索标题或作品 ID" value={query} onChange={event => setQuery(event.target.value)} /></label>
        <button className="btn-primary">搜索</button><button type="button" className="btn-outline px-3" aria-label="刷新黄果AI目录" disabled={loading} onClick={refresh}><RefreshCw size={16} /></button>
      </form>
    </div>
    {!keyword && section === 'category' && <nav className="flex flex-wrap gap-2" aria-label="黄果AI分类">{[['', '全部'], ...huangGuoCategories].map(([key, label]) => <button key={key} className={category === key ? 'btn-primary' : 'btn-outline'} aria-pressed={category === key} onClick={() => navigate({ category: key, page: '1', id: '' })}>{label}</button>)}</nav>}
    {ranking && <CatalogRankingHeader source="黄果 AI" title={huangGuoRanks.find(([key]) => key === rank)?.[1] || '热播榜'} total={total} options={huangGuoRanks.map(([value, label]) => ({ value, label }))} value={rank} onChange={value => navigate({ rank: value, page: '1', id: '' })} />}
    {!ranking && <p className="text-sm text-ink-50">{keyword ? `官网 + 本地资料 · 已显示 ${visibleRows.length} 部（去重）` : `本地目录 · 共 ${total} 部 · 已显示 ${visibleRows.length} 部`}</p>}
    {remoteError && <p role="alert" className="text-sm text-red-500">官网搜索失败，已保留本地和已加载结果。<button className="btn-outline ml-2" onClick={() => setRemoteRetry(v => v + 1)}>重试官网搜索</button></p>}
    {error && <p role="alert" className="text-sm text-red-500">资料读取失败，已保留现有结果。<button className="btn-outline ml-2" onClick={() => setRetry(v => v + 1)}>重试加载</button></p>}
    {loading && visibleRows.length === 0 ? <p role="status">加载黄果 AI 资料…</p> : ranking ? <ol className="space-y-3" aria-label="黄果AI排行榜">{visibleRows.map((work, index) => <CatalogRankingRow key={`${work.source_id}:${work.artwork_id}`} work={work} position={index + 1} artworkURL={work.artwork_id ? imageURL(huangguoaiAPI.artwork(work.artwork_id)) : ''} metadata={[huangGuoCategories.find(([key]) => key === work.source_category)?.[1], ...(work.tags ?? []), work.kind === 'movie' ? '电影' : work.hydrated ? `确认 ${work.confirmed_episode_count} 集` : '集数待确认'].filter(Boolean).join(' · ')} rankLabel={huangGuoRanks.find(([key]) => key === rank)?.[1] || '热播榜'} onOpen={() => navigate({ id: work.source_id })} />)}</ol> : <div className="grid grid-cols-2 gap-4 min-[480px]:grid-cols-3 sm:grid-cols-4 md:grid-cols-5">{visibleRows.map(work => <HuangGuoAICard key={`${work.source_id}:${work.artwork_id}`} work={work} onOpen={() => navigate({ id: work.source_id })} />)}</div>}
    {!loading && !error && !remoteError && visibleRows.length === 0 && <p className="py-12 text-center text-ink-50">暂无作品。管理员可在任务中心运行作品发现、资料刷新和榜单刷新。</p>}
    <div ref={loadMoreRef} data-testid="huangguoai-load-more" className="h-px" aria-hidden="true" />
    {loading && visibleRows.length > 0 && <p role="status" className="py-3 text-center text-sm text-ink-50">加载更多…</p>}
    {!loading && canLoadMore && <button className="btn-outline" onClick={loadMore}>加载更多</button>}
    {!loading && !error && !remoteError && !hasMore && visibleRows.length > 0 && <p className="py-3 text-center text-xs text-ink-50">当前结果已加载完</p>}
    {id && <HuangGuoAIDetailModal key={id} sourceID={id} summary={visibleRows.find(row => row.source_id === id)} onClose={() => navigate({ id: '' })} />}
  </section>
}
function mergeWorks(first: HuangGuoAIWork[], next: HuangGuoAIWork[]) {
  const merged = new Map<string, HuangGuoAIWork>()
  for (const work of [...first, ...next]) {
    const previous = merged.get(work.source_id)
    merged.set(work.source_id, { ...(!previous?.hydrated || work.hydrated ? work : previous), downloaded: Boolean(previous?.downloaded || work.downloaded) })
  }
  return Array.from(merged.values())
}

function HuangGuoAICard({ work, onOpen }: { work: HuangGuoAIWork; onOpen: () => void }) {
  const [failed, setFailed] = useState(false)
  const update = work.kind === 'movie' ? '电影' : work.hydrated ? `确认 ${work.confirmed_episode_count} 集` : work.episode_count > 0 ? `来源报告 ${work.episode_count} 集` : '集数未知'
  return <button className="group min-w-0 rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-4" onClick={onOpen} aria-label={`查看${work.title}`}>
    <div className="relative aspect-[2/3] overflow-hidden rounded-xl bg-gray-100 transition-shadow group-hover:shadow-poster-hover">
      {work.artwork_id && !failed ? <img src={imageURL(huangguoaiAPI.artwork(work.artwork_id))} alt={`${work.title}海报`} loading="lazy" decoding="async" className="h-full w-full object-cover transition-transform duration-300 motion-safe:group-hover:scale-105" onError={() => setFailed(true)} /> : <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center text-ink-50"><Film size={32} aria-hidden="true" /><span className="text-xs">暂无海报</span></div>}
      <div className="absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-black/80 to-transparent" />
      {!work.hydrated ? <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-white backdrop-blur">待补齐</span> : work.rating > 0 && <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-gold-300 backdrop-blur">{work.rating.toFixed(1)}</span>}
      {work.downloaded && <span data-huangguoai-downloaded className="absolute bottom-3 left-2 rounded-md bg-emerald-700/90 px-1 py-1 text-[10px] font-semibold text-white backdrop-blur xl:px-2 xl:text-xs" title="存在已完成的分集下载记录，不代表全剧下载完成、文件仍在或已入库">↓ 已下载</span>}
      <span className={`absolute bottom-3 right-2 max-w-[55%] truncate text-right font-medium text-white drop-shadow ${work.downloaded ? 'text-[10px] xl:text-xs' : 'text-xs'}`} title={update}>{update}</span>
    </div>
    <h2 className="mt-3 truncate text-sm font-semibold text-ink-600 transition-colors group-hover:text-brand-500" title={work.title}>{work.title}</h2><p className="mt-1 truncate text-xs text-ink-50">{[huangGuoCategories.find(([key]) => key === work.source_category)?.[1], ...(work.tags ?? []).slice(0, 2)].filter(Boolean).join(' · ') || '分类待补齐'}</p>
  </button>
}
