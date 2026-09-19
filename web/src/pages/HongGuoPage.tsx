import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { hongguoAPI, type HongGuoListWork, type HongGuoLibraryCard } from '../api/hongguo'
import { HongGuoDetailModal } from './HongGuoDetailModal'
import { HongGuoLibraryDetail } from './HongGuoLibraryDetail'
import { Film, RefreshCw, Search } from 'lucide-react'
import { imageURL } from '../api/client'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'
import { Select } from '../components/Select'
import { useAuthStore } from '../stores/auth'
import { HongGuoBatchActions } from './HongGuoBatchActions'
import { HongGuoGroupBadge } from './HongGuoGroupBadge'

const hongGuoCategories = {
  'real-drama': ['', '爱情', '年代', '逆袭', '传奇', '成长', '家庭', '家族', '萌宝', '悬疑', '惊悚', '恐怖', '志怪', '古装', '玄幻', '奇幻', '都市', '青春', '喜剧', '科幻', '灾难', '动作冒险', '战争', '综艺', '剧情'],
  'comic-drama': ['', '脑洞', '玄幻', '剧情', '末世', '豪门', '奇幻', '科幻', '冒险'],
  'ai-drama': ['', '脑洞', '玄幻', '剧情', '末世', '豪门', '奇幻', '科幻', '冒险'],
} as const
const hongGuoSourceCategories = [
  { value: '', label: '全部' },
  { value: 'real-drama', label: '真人剧' },
  { value: 'comic-drama', label: '漫剧' },
  { value: 'ai-drama', label: 'AI剧' },
  { value: 'other', label: '其它' },
] as const
const hongGuoRanks = [
  { value: 'hot-drama', label: '红果热播榜' },
  { value: 'hot-real-drama', label: '真人剧热播榜' },
  { value: 'hot-ai-drama', label: 'AI剧热播榜' },
  { value: 'hot-comic-drama', label: '漫剧热播榜' },
] as const
type HongGuoRank = typeof hongGuoRanks[number]['value']
type HongGuoSourceCategory = typeof hongGuoSourceCategories[number]['value']

export function HongGuoLibraryView({ libraryID, title }: { libraryID: string; title: string }) {
  const accessKey = useMediaAccessKey()
  return <HongGuoLibraryContent key={`${libraryID}:${accessKey}`} libraryID={libraryID} title={title} />
}

function HongGuoLibraryContent({ libraryID, title }: { libraryID: string; title: string }) {
  const [params, setParams] = useSearchParams()
  const sourceID = params.get('hongguo_id') ?? ''
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 1000000 ? rawPage : 1
  const [data, setData] = useState<{ page: number; items: HongGuoLibraryCard[]; total: number } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (rawPage === page && params.getAll('page').length <= 1) return
    const next = new URLSearchParams(params); next.set('page', String(page)); setParams(next, { replace: true })
  }, [params, setParams, rawPage, page])
  useEffect(() => {
    if (sourceID) return
    const controller = new AbortController(); setError(false)
    void hongguoAPI.library(libraryID, page, controller.signal).then((result) => { if (!controller.signal.aborted) setData({ ...result, page }) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [libraryID, page, retry, sourceID])
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('page', String(value)); setParams(next) }
  if (sourceID) return <HongGuoLibraryDetail key={sourceID} sourceID={sourceID} onClose={() => { const next = new URLSearchParams(params); next.delete('hongguo_id'); next.delete('media_page'); setParams(next) }} />
  return <section className="space-y-5"><header><h1 className="font-display text-3xl font-bold">{title}</h1><p className="text-sm text-ink-50">红果短剧 · 已匹配媒体 · 按人工聚合展示</p></header>
    {error ? <p role="alert">媒体库读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : data?.page !== page ? <p role="status">加载中…</p> : <>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">{data.items.map((item) => <button key={item.id} className="card overflow-hidden text-left" onClick={() => { const next = new URLSearchParams(params); next.set('hongguo_id', item.source_id); next.delete('media_page'); setParams(next) }}>
        {item.artwork_id && <img className="aspect-[2/3] w-full object-cover" loading="lazy" src={imageURL(hongguoAPI.artwork(item.artwork_id))} alt={`${item.title}海报`} />}<div className="p-3"><h2 className="font-semibold">{item.title}</h2><p className="text-sm text-ink-50">{item.kind === 'movie' ? '电影' : '剧集'}</p></div>
      </button>)}</div>
      {data.items.length === 0 && <p>暂无已匹配媒体。请先导入红果资料，并使用来源 ID 扫描本地文件或 STRM。</p>}
      <div className="flex items-center gap-3"><button className="btn-outline" disabled={page <= 1} onClick={() => goPage(page - 1)}>上一页</button><span>第 {page} 页 · 共 {data.total} 项</span><button className="btn-outline" disabled={page * 50 >= data.total} onClick={() => goPage(page + 1)}>下一页</button></div>
    </>}
  </section>
}

export function HongGuoPage() {
  const accessKey = useMediaAccessKey()
  const [params, setParams] = useSearchParams()
  const keyword = params.get('keyword') ?? ''
  const sourceID = params.get('id') ?? ''
  const section = params.get('section') === 'rank' ? 'rank' : 'category'
  const rawSourceCategory = params.get('source') ?? ''
  const sourceCategory = hongGuoSourceCategories.some((item) => item.value === rawSourceCategory) ? rawSourceCategory as HongGuoSourceCategory : ''
  const rawCategory = params.get('category') ?? ''
  const categories = sourceCategory && sourceCategory !== 'other' ? hongGuoCategories[sourceCategory] : ['']
  const category = categories.some((item) => item === rawCategory) ? rawCategory : ''
  const rawRank = params.get('rank') ?? 'hot-drama'
  const rank = hongGuoRanks.some((item) => item.value === rawRank) ? rawRank as HongGuoRank : 'hot-drama'
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage >= 1 && rawPage <= 1000000 ? rawPage : 1
  useEffect(() => {
    const next = new URLSearchParams(params)
    for (const key of ['keyword', 'id', 'page', 'section', 'source', 'category', 'rank']) {
      const values = params.getAll(key)
      if (values.length > 1) next.set(key, values[0])
    }
    if (rawPage !== page) next.set('page', String(page))
    if (params.get('section') && params.get('section') !== section) next.delete('section')
    if (rawSourceCategory !== sourceCategory) next.delete('source')
    if (rawCategory !== category) next.delete('category')
    if (rawRank !== rank) next.delete('rank')
    if (section === 'rank') { next.delete('source'); next.delete('category') }
    else next.delete('rank')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, setParams, rawPage, page, section, rawSourceCategory, sourceCategory, rawCategory, category, rawRank, rank])
  return <HongGuoContent key={`${accessKey}:${keyword}:${section}:${sourceCategory}:${category}:${rank}:${page}`} sourceID={sourceID} keyword={keyword} section={section} sourceCategory={section === 'category' ? sourceCategory : ''} category={section === 'category' ? category : ''} rank={section === 'rank' ? rank : ''} page={page} navigate={(changes) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(changes)) { if (value) next.set(key, value); else next.delete(key) }
    setParams(next)
  }} />
}

function HongGuoContent({ sourceID, keyword, section, sourceCategory, category, rank, page, navigate }: { sourceID: string; keyword: string; section: 'category' | 'rank'; sourceCategory: HongGuoSourceCategory; category: string; rank: '' | HongGuoRank; page: number; navigate: (changes: Record<string, string>) => void }) {
  const admin = useAuthStore((s) => s.user?.role === 'admin')
  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<HongGuoListWork[]>([])
  const [batchBusy, setBatchBusy] = useState(false)
  const [rows, setRows] = useState<HongGuoListWork[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [revision, setRevision] = useState(0)
  const [query, setQuery] = useState(keyword)
  const [localRetry, setLocalRetry] = useState(0)
  const [remoteRows, setRemoteRows] = useState<HongGuoListWork[]>([])
  const [remoteLoading, setRemoteLoading] = useState(Boolean(keyword))
  const [remoteError, setRemoteError] = useState(false)
  const [remoteRetry, setRemoteRetry] = useState(0)
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const firstPage = keyword ? 1 : page
  const [catalogPage, setCatalogPage] = useState(firstPage)
  const [hasMore, setHasMore] = useState(false)
  const loadMoreRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      setLoading(true); setError(false)
      try {
        const data = await hongguoAPI.list(keyword, keyword ? '' : sourceCategory, keyword ? '' : category, keyword ? '' : rank, catalogPage, controller.signal)
        if (!controller.signal.aborted) {
          setRows((current) => catalogPage === firstPage ? data.items : Array.from(new Map([...current, ...data.items].map((item) => [item.source_id, item])).values()))
          setTotal(data.total); setHasMore(catalogPage * 50 < data.total)
        }
      } catch { if (!controller.signal.aborted) setError(true) }
      finally { if (!controller.signal.aborted) setLoading(false) }
    }
    void load()
    return () => controller.abort()
  }, [keyword, sourceCategory, category, rank, firstPage, catalogPage, revision, localRetry])
  useEffect(() => {
    if (!keyword) return
    const controller = new AbortController()
    setRemoteLoading(true); setRemoteError(false)
    void hongguoAPI.search(keyword, controller.signal).then((data) => {
      if (!controller.signal.aborted) setRemoteRows(data.items)
    }).catch(() => { if (!controller.signal.aborted) setRemoteError(true) })
      .finally(() => { if (!controller.signal.aborted) setRemoteLoading(false) })
    return () => controller.abort()
  }, [keyword, revision, remoteRetry])
  useEffect(() => {
    const target = loadMoreRef.current
    if (!target || sourceID || loading || error || !hasMore) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) { observer.disconnect(); setLoading(true); setCatalogPage((current) => current + 1) }
    }, { rootMargin: '320px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [hasMore, loading, error, sourceID])
  useEffect(() => { let active = true; void hongguoAPI.status().then((s) => { if (active) setEnabled(s.enabled) }).catch(() => undefined); return () => { active = false } }, [])

  const merged = new Map<string, HongGuoListWork>()
  for (const work of [...remoteRows, ...rows]) {
    const previous = merged.get(work.source_id)
    merged.set(work.source_id, { ...(!previous?.hydrated || work.hydrated ? work : previous), downloaded: !!(previous?.downloaded || work.downloaded) })
  }
  // 成功写入优先于更早发出、稍后才返回的列表请求；显式刷新再以服务端关系为准。
  const visibleRows = Array.from(merged.values())
  const refresh = () => { setRows([]); setRemoteRows([]); setLoading(true); setHasMore(false); setCatalogPage(firstPage); setRevision((value) => value + 1) }

  return <div className="space-y-6">
    {<><div className="flex flex-wrap items-center justify-between gap-3">
      <nav aria-label="红果发现模式" className="flex gap-2">
        <button type="button" className={section === 'category' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'category'} onClick={() => navigate({ keyword: '', section: '', category: '', rank: '', page: '1' })}>分类</button>
        <button type="button" className={section === 'rank' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'rank'} onClick={() => navigate({ keyword: '', section: 'rank', source: '', category: '', rank: 'hot-drama', page: '1' })}>榜单</button>
      </nav>
      <form className="flex w-full gap-2 sm:w-auto" onSubmit={(e) => { e.preventDefault(); if (query.trim() === keyword) { refresh(); return } navigate({ keyword: query.trim(), source: '', category: '', section: '', rank: '', page: '1' }) }}>
        <label className="relative min-w-0 flex-1"><Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-50" /><input type="search" maxLength={100} className="input-field w-full pl-10 sm:w-72" aria-label="搜索红果资料" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索官网标题或作品 ID" /></label><button className="btn-primary">搜索</button>
        <button type="button" className="btn-outline px-3" aria-label="刷新红果目录" disabled={loading || remoteLoading} onClick={refresh}><RefreshCw size={16} /></button>
      </form>
    </div>
      {keyword ? <div className="space-y-1 text-sm text-ink-50"><p>官网首屏 + 本地资料 · 已显示 {visibleRows.length} 部（去重）</p><p className="text-xs">下滑加载本地匹配作品，不代表官网全部搜索结果。</p>{remoteLoading && <p role="status">正在搜索官网…</p>}</div> : <div className="space-y-3">
        {section === 'category' && <div className="flex flex-wrap gap-2">{hongGuoSourceCategories.map((item) => <button key={item.value || 'all'} type="button" aria-pressed={sourceCategory === item.value} className={sourceCategory === item.value ? 'btn-primary' : 'btn-outline'} onClick={() => navigate({ source: item.value, category: '', page: '1' })}>{item.label}</button>)}</div>}
        {section === 'category' && sourceCategory && sourceCategory !== 'other' ? <div className="flex flex-wrap gap-2">{hongGuoCategories[sourceCategory].map((item) => <button key={item || 'all'} type="button" aria-pressed={category === item} className={category === item ? 'btn-primary' : 'btn-outline'} onClick={() => navigate({ section: '', category: item, rank: '', page: '1' })}>{item || '全部'}</button>)}</div> : section === 'rank' ? <Select aria-label="选择红果榜单" className="input-field min-h-10 w-full sm:w-64" value={rank} onChange={(value) => navigate({ section: 'rank', source: '', category: '', rank: value, page: '1' })}>{hongGuoRanks.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</Select> : null}
        <p className="text-sm text-ink-50">{section === 'category' && `${hongGuoSourceCategories.find((item) => item.value === sourceCategory)?.label} · ${category || '全部'}分类 · `}<span className="font-semibold text-ink-600">{loading && rows.length === 0 ? '…' : total}</span></p>
      </div>}</>}
    {admin && <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className="btn-outline" aria-pressed={selecting} disabled={batchBusy} onClick={() => { setSelecting((value) => !value); setSelected([]) }}>{selecting ? '退出多选' : '多选'}</button>
        {selecting && <HongGuoBatchActions selected={selected} onSelectedChange={setSelected} enabled={enabled === true} busy={batchBusy} onBusyChange={setBatchBusy} />}
      </div>
      {selecting && <p className="text-xs text-ink-50">待补齐作品暂不可选，切换搜索或分类会清空选择。系列关系由红果官方资料提供。</p>}
    </div>}
    {remoteError && <p role="alert" className="text-sm text-red-500">官网搜索失败，已保留本地结果。<button className="btn-outline ml-2" onClick={() => setRemoteRetry((value) => value + 1)}>重试官网搜索</button></p>}
    {error && <p role="alert" className="text-sm text-red-500">{keyword ? '本地资料加载失败，已保留现有结果。' : '资料加载失败，已保留现有结果。'}<button className="btn-outline ml-2" onClick={() => setLocalRetry((value) => value + 1)}>重试加载</button></p>}
    {(loading || remoteLoading) && visibleRows.length === 0 ? <p role="status">加载红果资料中…</p> : <>
      <div className="grid grid-cols-2 gap-4 min-[480px]:grid-cols-3 sm:grid-cols-4 md:grid-cols-5">{visibleRows.map((work) => <HongGuoPosterCard key={`${work.source_id}:${revision}`} work={work} showSourceCategory={Boolean(keyword) || !sourceCategory} selecting={admin && selecting} selected={selected.some((item) => item.source_id === work.source_id)} disabled={admin && selecting && (batchBusy || !work.hydrated)} onOpen={() => {
        if (!admin || !selecting) { navigate({ id: work.source_id, media_page: '' }); return }
        setSelected((current) => current.some((item) => item.source_id === work.source_id) ? current.filter((item) => item.source_id !== work.source_id) : [...current, work])
      }} />)}</div>
      {visibleRows.length === 0 && !error && !remoteError && <p className="py-12 text-center text-ink-50">{keyword ? '没有找到匹配的作品，试试其他标题或作品 ID。' : '暂无完整资料。管理员可按 ID 导入，或先运行作品发现，再运行资料刷新。'}</p>}
      <div ref={loadMoreRef} data-testid="hongguo-load-more" className="h-px" aria-hidden="true" />
      {loading && <p role="status" className="py-3 text-center text-sm text-ink-50">加载更多…</p>}
      {keyword && !loading && !error && !hasMore && <p className="py-3 text-center text-xs text-ink-50">本地匹配已加载完，官网仅展示首屏结果。</p>}
    </>}
    {sourceID && <HongGuoDetailModal key={sourceID} sourceID={sourceID} summary={visibleRows.find((work) => work.source_id === sourceID)} enabled={enabled === true} onClose={() => navigate({ id: '', media_page: '' })} onCategorySaved={(value) => {
      setRemoteRows((current) => current.map((work) => work.source_id === sourceID ? { ...work, source_category: value } : work))
      if (!keyword && sourceCategory && value !== sourceCategory && rows.some((work) => work.source_id === sourceID)) setTotal((current) => Math.max(0, current - 1))
      setRows((current) => current.map((work) => work.source_id === sourceID ? { ...work, source_category: value } : work).filter((work) => keyword || !sourceCategory || work.source_category === sourceCategory))
    }} />}
  </div>
}

function HongGuoPosterCard({ work, showSourceCategory, onOpen, selecting = false, selected = false, disabled = false }: { work: HongGuoListWork; showSourceCategory: boolean; onOpen: () => void; selecting?: boolean; selected?: boolean; disabled?: boolean }) {
  const [failed, setFailed] = useState(false)
  return <div className="relative min-w-0"><button type="button" onClick={onOpen} disabled={disabled} aria-pressed={selecting ? selected : undefined} className={`group w-full min-w-0 rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-4 disabled:opacity-50 ${selecting && selected ? 'ring-2 ring-brand-500 ring-offset-4 ring-offset-[var(--app-bg)]' : ''}`} aria-label={`${selecting ? '选择' : '查看'}${work.title}`}>
    <div className="relative aspect-[2/3] overflow-hidden rounded-xl bg-gray-100 transition-shadow group-hover:shadow-poster-hover">
      {work.artwork_id && !failed ? <img className="h-full w-full object-cover transition-transform duration-300 motion-safe:group-hover:scale-105" loading="lazy" decoding="async" src={imageURL(hongguoAPI.artwork(work.artwork_id))} alt={`${work.title}海报`} onError={() => setFailed(true)} /> : <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center text-ink-50"><Film size={32} aria-hidden="true" /><span className="text-xs">暂无海报</span></div>}
      <div className="absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-black/80 to-transparent" />
      {!work.hydrated ? <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-white backdrop-blur">待补齐</span> : work.rating > 0 && <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-gold-300 backdrop-blur">{work.rating.toFixed(1)}</span>}
      {selecting && <span aria-hidden="true" className={`absolute right-2 top-2 flex h-6 w-6 items-center justify-center rounded-full border text-white ${selected ? 'border-brand-500 bg-brand-500' : 'border-white bg-black/55'}`}>{selected ? '✓' : ''}</span>}
      {work.downloaded && <span data-hongguo-downloaded className="absolute bottom-3 left-2 rounded-md bg-emerald-700/90 px-1 py-1 text-[10px] font-semibold text-white backdrop-blur xl:px-2 xl:text-xs" title="存在已完成的分集下载记录，不代表全剧下载完成、文件仍在或已入库">↓ 已下载</span>}
    </div>
    <h2 className="mt-3 truncate text-sm font-semibold text-ink-600 transition-colors group-hover:text-brand-500" title={work.title}>{work.title}</h2>
    <p className="mt-1 truncate text-xs text-ink-50">{[showSourceCategory && hongGuoSourceCategories.find((item) => item.value === work.source_category)?.label, ...(work.tags ?? []).slice(0, 2)].filter(Boolean).join(' · ') || (work.hydrated ? work.kind === 'movie' ? '电影' : '剧集' : '待补齐')}</p>
  </button><div className="pointer-events-none absolute inset-x-0 top-0 flex aspect-[2/3] items-end justify-end px-3 pb-3"><div className={`flex min-w-0 items-center gap-1 font-medium text-white drop-shadow ${work.downloaded ? 'text-[10px] xl:text-xs' : 'text-xs'}`}>
    {work.group_id && <HongGuoGroupBadge key={work.group_id} groupID={work.group_id} sourceID={work.source_id} />}
    <span data-hongguo-episode-label className="min-w-0 truncate text-right" title={work.update_text || (work.episode_count > 0 ? `已更新 ${work.episode_count} 集` : '集数未知')}>{work.update_text || (work.episode_count > 0 ? `已更新 ${work.episode_count} 集` : '集数未知')}</span>
  </div></div></div>
}
