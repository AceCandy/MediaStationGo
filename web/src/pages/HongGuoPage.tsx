import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { hongguoAPI, type HongGuoDetail, type HongGuoListWork, type HongGuoGroup, type HongGuoGroupInput, type HongGuoLibraryCard } from '../api/hongguo'
import { Film, RefreshCw, Search } from 'lucide-react'
import { imageURL } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import type { Media } from '../types'

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
type HongGuoCategorySource = keyof typeof hongGuoCategories

export function HongGuoLibraryView({ libraryID, title }: { libraryID: string; title: string }) {
  const accessKey = useMediaAccessKey()
  return <HongGuoLibraryContent key={`${libraryID}:${accessKey}`} libraryID={libraryID} title={title} />
}

function HongGuoLibraryContent({ libraryID, title }: { libraryID: string; title: string }) {
  const [params, setParams] = useSearchParams()
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
    const controller = new AbortController(); setError(false)
    void hongguoAPI.library(libraryID, page, controller.signal).then((result) => { if (!controller.signal.aborted) setData({ ...result, page }) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [libraryID, page, retry])
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('page', String(value)); setParams(next) }
  return <section className="space-y-5"><header><h1 className="font-display text-3xl font-bold">{title}</h1><p className="text-sm text-ink-50">红果短剧 · 已匹配媒体 · 按人工聚合展示</p></header>
    {error ? <p role="alert">媒体库读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : data?.page !== page ? <p role="status">加载中…</p> : <>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">{data.items.map((item) => <Link key={item.id} className="card overflow-hidden" to={`/discover?system=hongguo&id=${encodeURIComponent(item.source_id)}`}>
        {item.artwork_id && <img className="aspect-[2/3] w-full object-cover" loading="lazy" src={imageURL(hongguoAPI.artwork(item.artwork_id))} alt={`${item.title}海报`} />}<div className="p-3"><h2 className="font-semibold">{item.title}</h2><p className="text-sm text-ink-50">{item.kind === 'movie' ? '电影' : '剧集'}</p></div>
      </Link>)}</div>
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
  return <HongGuoContent key={`${accessKey}:${sourceID}:${keyword}:${section}:${sourceCategory}:${category}:${rank}:${page}`} sourceID={sourceID} keyword={keyword} section={section} sourceCategory={section === 'category' ? sourceCategory : ''} category={section === 'category' ? category : ''} rank={section === 'rank' ? rank : ''} page={page} navigate={(changes) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(changes)) { if (value) next.set(key, value); else next.delete(key) }
    setParams(next)
  }} />
}

function HongGuoContent({ sourceID, keyword, section, sourceCategory, category, rank, page, navigate }: { sourceID: string; keyword: string; section: 'category' | 'rank'; sourceCategory: HongGuoSourceCategory; category: string; rank: '' | HongGuoRank; page: number; navigate: (changes: Record<string, string>) => void }) {
  const admin = useAuthStore((s) => s.user?.role === 'admin')
  const [rows, setRows] = useState<HongGuoListWork[]>([])
  const [total, setTotal] = useState(0)
  const [detail, setDetail] = useState<HongGuoDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [notFound, setNotFound] = useState(false)
  const [revision, setRevision] = useState(0)
  const [query, setQuery] = useState(keyword)
  const [busy, setBusy] = useState(false)
  const [categoryWork, setCategoryWork] = useState<HongGuoListWork | null>(null)
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [catalogPage, setCatalogPage] = useState(page)
  const [hasMore, setHasMore] = useState(false)
  const loadMoreRef = useRef<HTMLDivElement>(null)
  const activeRef = useRef(true)
  useEffect(() => { activeRef.current = true; return () => { activeRef.current = false } }, [])
  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      setLoading(true); setError(false); setNotFound(false)
      try {
        if (sourceID) { const data = await hongguoAPI.detail(sourceID, controller.signal); if (!controller.signal.aborted) setDetail(data) }
        else { const data = keyword ? await hongguoAPI.search(keyword, controller.signal) : await hongguoAPI.list('', sourceCategory, category, rank, catalogPage, controller.signal); if (!controller.signal.aborted) { setRows((current) => keyword || catalogPage === page ? data.items : Array.from(new Map([...current, ...data.items].map((item) => [item.source_id, item])).values())); setTotal(data.total); setHasMore(!keyword && catalogPage * 50 < data.total) } }
      } catch (err: unknown) { if (!controller.signal.aborted) { setError(true); setNotFound(sourceID !== '' && (err as { response?: { status?: number } })?.response?.status === 404) } }
      finally { if (!controller.signal.aborted) setLoading(false) }
    }
    void load()
    return () => controller.abort()
  }, [sourceID, keyword, sourceCategory, category, rank, page, catalogPage, revision])
  useEffect(() => {
    const target = loadMoreRef.current
    if (!target || sourceID || loading || !hasMore) return
    const observer = new IntersectionObserver(([entry]) => {
      if (entry.isIntersecting) setCatalogPage((current) => current + 1)
    }, { rootMargin: '320px' })
    observer.observe(target)
    return () => observer.disconnect()
  }, [hasMore, loading, sourceID])
  useEffect(() => { let active = true; void hongguoAPI.status().then((s) => { if (active) setEnabled(s.enabled) }).catch(() => undefined); return () => { active = false } }, [])

  const refresh = async (id: string) => {
    if (busy) return
    if (!/^[1-9][0-9]{0,31}$/.test(id)) { toast.error('请输入有效红果作品 ID'); return }
    setBusy(true)
    try { await hongguoAPI.refresh(id); if (!activeRef.current) return; toast.success('资料已更新，图片可在任务中心下载'); if (sourceID === id) setRevision((v) => v + 1); else navigate({ id, media_page: '' }) }
    catch { if (activeRef.current) toast.error('刷新失败或已有红果任务运行，请查看任务中心') }
    finally { if (activeRef.current) setBusy(false) }
  }
  const setSourceCategory = async (value: HongGuoCategorySource) => {
    if (!categoryWork || busy) return
    setBusy(true)
    try {
      await hongguoAPI.setSourceCategory(categoryWork.source_id, value)
      if (!activeRef.current) return
      setRows((current) => current.filter((work) => work.source_id !== categoryWork.source_id))
      setTotal((current) => Math.max(0, current - 1))
      setCategoryWork(null)
      toast.success('分类已保存')
    } catch { if (activeRef.current) toast.error('分类保存失败') }
    finally { if (activeRef.current) setBusy(false) }
  }
  const poster = detail?.artwork.find((a) => a.work_id === detail.id)
  return <div className="space-y-6">
    {sourceID ? <button className="btn-outline" onClick={() => navigate({ id: '', media_page: '' })}>返回资料列表</button> : <><div className="flex flex-wrap items-center justify-between gap-3">
      <nav aria-label="红果发现模式" className="flex gap-2">
        <button type="button" className={section === 'category' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'category'} onClick={() => navigate({ keyword: '', section: '', category: '', rank: '', page: '1' })}>分类</button>
        <button type="button" className={section === 'rank' ? 'btn-primary' : 'btn-outline'} aria-pressed={section === 'rank'} onClick={() => navigate({ keyword: '', section: 'rank', source: '', category: '', rank: 'hot-drama', page: '1' })}>榜单</button>
      </nav>
      <form className="flex w-full gap-2 sm:w-auto" onSubmit={(e) => { e.preventDefault(); if (query.trim() === keyword) { setRevision((v) => v + 1); return } navigate({ keyword: query.trim(), source: '', category: '', section: '', rank: '', page: '1' }) }}>
        <label className="relative min-w-0 flex-1"><Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-50" /><input type="search" maxLength={100} className="input-field w-full pl-10 sm:w-72" aria-label="搜索红果资料" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索官网标题或作品 ID" /></label><button className="btn-primary">搜索</button>
        <button type="button" className="btn-outline px-3" aria-label="刷新红果目录" disabled={loading} onClick={() => { setRows([]); setCatalogPage(page); setRevision((v) => v + 1) }}><RefreshCw size={16} /></button>
      </form>
    </div>
      {keyword ? <p className="text-sm text-ink-50">官网搜索 · 本次返回 {loading ? '…' : total} 项{busy ? ' · 正在补录资料…' : admin ? ' · 点击未收录作品可补齐资料' : ''}</p> : <div className="space-y-3">
        {section === 'category' && <div className="flex flex-wrap gap-2">{hongGuoSourceCategories.map((item) => <button key={item.value || 'all'} type="button" aria-pressed={sourceCategory === item.value} className={sourceCategory === item.value ? 'btn-primary' : 'btn-outline'} onClick={() => navigate({ source: item.value, category: '', page: '1' })}>{item.label}</button>)}</div>}
        {section === 'category' && sourceCategory && sourceCategory !== 'other' ? <div className="flex flex-wrap gap-2">{hongGuoCategories[sourceCategory].map((item) => <button key={item || 'all'} type="button" aria-pressed={category === item} className={category === item ? 'btn-primary' : 'btn-outline'} onClick={() => navigate({ section: '', category: item, rank: '', page: '1' })}>{item || '全部'}</button>)}</div> : section === 'rank' ? <Select aria-label="选择红果榜单" className="input-field min-h-10 w-full sm:w-64" value={rank} onChange={(value) => navigate({ section: 'rank', source: '', category: '', rank: value, page: '1' })}>{hongGuoRanks.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</Select> : null}
        <p className="text-sm text-ink-50">{section === 'category' && `${hongGuoSourceCategories.find((item) => item.value === sourceCategory)?.label} · ${category || '全部'}分类 · `}<span className="font-semibold text-ink-600">{loading && rows.length === 0 ? '…' : total}</span></p>
      </div>}</>}
    {loading && (sourceID || rows.length === 0) ? <div role="status" aria-label="加载红果资料" className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5">{Array.from({ length: 16 }, (_, i) => <div key={i} className="skeleton aspect-[2/3] rounded-xl" />)}</div> : notFound ? <p role="alert">红果资料不存在，已等待后续重试。</p> : error && rows.length === 0 ? <div role="alert"><p>资料读取失败。</p><button className="btn-outline" onClick={() => setRevision((v) => v + 1)}>重试</button></div> : detail ? <section className="glass-panel space-y-4">
      <div className="flex flex-col gap-5 sm:flex-row">{poster && <img className="w-40 self-start rounded-lg" src={imageURL(hongguoAPI.artwork(poster.id))} alt={`${detail.title}海报`} />}<div className="min-w-0 space-y-3"><h2 className="text-2xl font-bold">{detail.title}</h2><p className="break-all text-sm text-ink-50">红果短剧 ID：{detail.source_id}</p><p>{detail.kind === 'movie' ? '电影' : '剧集'} · {detail.update_text || `已更新 ${detail.episode_count} 集`}</p><p>{detail.rating ? `评分 ${detail.rating}（${detail.rating_count} 人）` : '暂无评分'}</p><p>红果上线：{detail.first_visible_at ? new Date(detail.first_visible_at).toLocaleString() : '未知'}</p><p className="text-sm text-ink-50">上线时间不代表全网首播时间。</p><p>{detail.tags.join(' / ')}</p></div></div>
      <p className="whitespace-pre-wrap break-words">{detail.overview || '暂无简介'}</p>
      <HongGuoFiles key={`${detail.source_id}:${revision}`} sourceID={detail.source_id} />
      <HongGuoFavorite key={detail.source_id} sourceID={detail.source_id} />
      {admin && <button className="btn-outline" disabled={busy || enabled === false} onClick={() => void refresh(sourceID)}>刷新资料</button>}
      {detail.kind === 'series' && <HongGuoGrouping key={`${detail.source_id}:${revision}`} detail={detail} admin={admin} navigate={navigate} onSaved={() => setRevision((v) => v + 1)} />}
      <h3 className="text-lg font-semibold">演职员</h3><div className="grid grid-cols-2 gap-3 sm:grid-cols-4">{detail.credits.map((c) => { const avatar = detail.artwork.find((a) => a.person_id === c.person_id); return <div key={c.person_id} className="min-w-0">{avatar && <img className="h-20 w-20 rounded-full object-cover" loading="lazy" src={imageURL(hongguoAPI.artwork(avatar.id))} alt={c.person.name} />}<p>{c.person.name}</p><p className="break-words text-sm text-ink-50">{c.subtitle}</p></div> })}</div>
      <p className="break-all text-sm text-ink-50">匹配标识：[hongguo-{detail.source_id}]{detail.kind === 'series' ? ' / S01E001' : ''}</p>
    </section> : <>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5">{rows.map((work) => <HongGuoPosterCard key={`${work.source_id}:${revision}`} work={work} showSourceCategory={Boolean(keyword) || !sourceCategory} onOpen={() => { if (busy) return; if (sourceCategory === 'other' && admin) { setCategoryWork(work); return } if (!work.hydrated) { if (keyword && admin) { if (enabled === false) { toast.error('红果来源已停用'); return } void refresh(work.source_id); return } toast.error(keyword ? '此作品尚未收录，请管理员补录资料' : '红果资料不存在，已等待后续重试'); return } navigate({ id: work.source_id, media_page: '' }) }} />)}</div>
      {error && <p role="alert" className="text-center text-sm text-red-500">加载更多失败 <button className="btn-outline ml-2" onClick={() => setRevision((v) => v + 1)}>重试</button></p>}
      {rows.length === 0 && <p className="py-12 text-center text-ink-50">{keyword ? '没有找到匹配的作品，试试其他标题或作品 ID。' : '暂无完整资料。管理员可按 ID 导入，或先运行作品发现，再运行资料刷新。'}</p>}
      <div ref={loadMoreRef} data-testid="hongguo-load-more" className="h-px" aria-hidden="true" />
      {loading && <p role="status" className="py-3 text-center text-sm text-ink-50">加载更多…</p>}
    </>}
    {categoryWork && <ModalShell onClose={busy ? undefined : () => setCategoryWork(null)} maxWidth="max-w-sm" className="space-y-4 p-5" ariaLabel={`设置${categoryWork.title}分类`}><div><h2 className="text-lg font-semibold">设置分类</h2><p className="mt-1 text-sm text-ink-50">{categoryWork.title}</p></div><div className="grid gap-2">{hongGuoSourceCategories.filter((item) => item.value !== '' && item.value !== 'other').map((item) => <button key={item.value} type="button" className="btn-outline" disabled={busy} onClick={() => void setSourceCategory(item.value as HongGuoCategorySource)}>{item.label}</button>)}</div><button type="button" className="btn-outline w-full" disabled={busy} onClick={() => setCategoryWork(null)}>取消</button></ModalShell>}
  </div>
}

function HongGuoPosterCard({ work, showSourceCategory, onOpen }: { work: HongGuoListWork; showSourceCategory: boolean; onOpen: () => void }) {
  const [failed, setFailed] = useState(false)
  return <button type="button" onClick={onOpen} className="group min-w-0 rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-4" aria-label={`查看${work.title}`}>
    <div className="relative aspect-[2/3] overflow-hidden rounded-xl bg-gray-100 transition-shadow group-hover:shadow-poster-hover">
      {work.artwork_id && !failed ? <img className="h-full w-full object-cover transition-transform duration-300 motion-safe:group-hover:scale-105" loading="lazy" decoding="async" src={imageURL(hongguoAPI.artwork(work.artwork_id))} alt={`${work.title}海报`} onError={() => setFailed(true)} /> : <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center text-ink-50"><Film size={32} aria-hidden="true" /><span className="text-xs">暂无海报</span></div>}
      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 to-transparent px-3 pb-3 pt-10 text-right text-xs font-medium text-white">{work.update_text || (work.episode_count > 0 ? `已更新 ${work.episode_count} 集` : '集数未知')}</div>
      {!work.hydrated ? <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-white backdrop-blur">待补齐</span> : work.rating > 0 && <span className="absolute left-2 top-2 rounded-md bg-black/55 px-2 py-1 text-xs font-semibold text-gold-300 backdrop-blur">{work.rating.toFixed(1)}</span>}
    </div>
    <h2 className="mt-3 truncate text-sm font-semibold text-ink-600 transition-colors group-hover:text-brand-500" title={work.title}>{work.title}</h2>
    <p className="mt-1 truncate text-xs text-ink-50">{[showSourceCategory && hongGuoSourceCategories.find((item) => item.value === work.source_category)?.label, ...(work.tags ?? []).slice(0, 2)].filter(Boolean).join(' · ') || (work.hydrated ? work.kind === 'movie' ? '电影' : '剧集' : '待补齐')}</p>
  </button>
}

function HongGuoFavorite({ sourceID }: { sourceID: string }) {
  const [favorite, setFavorite] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(false)
  const [retry, setRetry] = useState(0)
  const [error, setError] = useState(false)
  useEffect(() => {
    const controller = new AbortController(); setError(false)
    void hongguoAPI.favorite(sourceID, controller.signal).then((value) => { if (!controller.signal.aborted) setFavorite(value) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [sourceID, retry])
  if (error) return <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试读取收藏</button>
  return <button className="btn-outline" disabled={favorite === null || busy} onClick={() => {
    if (favorite === null || busy) return
    setBusy(true)
    void hongguoAPI.setFavorite(sourceID, !favorite).then(() => setFavorite(!favorite)).catch(() => toast.error('收藏更新失败')).finally(() => setBusy(false))
  }}>{favorite ? '取消收藏此源作品' : '收藏此源作品'}</button>
}

function HongGuoFiles({ sourceID }: { sourceID: string }) {
  const [params, setParams] = useSearchParams()
  const rawPage = Number(params.get('media_page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 1000000 ? rawPage : 1
  const [result, setResult] = useState<{ page: number; items: Media[]; total: number } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (rawPage === page && params.getAll('media_page').length <= 1) return
    const next = new URLSearchParams(params); next.set('media_page', String(page)); setParams(next, { replace: true })
  }, [params, setParams, page, rawPage])
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void hongguoAPI.media(sourceID, page, controller.signal).then((data) => {
      if (!controller.signal.aborted) setResult({ ...data, page })
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [sourceID, page, retry])
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('media_page', String(value)); setParams(next) }
  return <section className="space-y-3"><h3 className="text-lg font-semibold">本地媒体与 STRM</h3>
    {error ? <p role="alert">媒体读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : result?.page !== page ? <p role="status">加载媒体中…</p> : <>
      {result.items.length === 0 ? <p className="text-sm text-ink-50">暂无可访问的已匹配媒体。导入资料并扫描红果短剧类型媒体库后会显示在这里。</p> : <div className="grid gap-2 sm:grid-cols-2">{result.items.map((media) => <Link className="card min-w-0 p-3" key={media.id} to={`/play/${encodeURIComponent(media.id)}`} state={{ from: `/discover?${params.toString()}` }}><span className="font-semibold">播放 · {media.episode_num > 0 ? `S${media.season_num}E${String(media.episode_num).padStart(3, '0')}` : media.title}</span><p className="break-all text-xs text-ink-50">{media.relative_path || media.title}</p></Link>)}</div>}
      {result.total > 50 && <div className="flex flex-wrap items-center gap-2"><button className="btn-outline" disabled={page <= 1} onClick={() => goPage(page - 1)}>上一页媒体</button><span>第 {page} 页 · 共 {result.total} 个文件</span><button className="btn-outline" disabled={page * 50 >= result.total} onClick={() => goPage(page + 1)}>下一页媒体</button></div>}
    </>}
  </section>
}

function HongGuoGrouping({ detail, admin, navigate, onSaved }: { detail: HongGuoDetail; admin: boolean; navigate: (changes: Record<string, string>) => void; onSaved: () => void }) {
  const groupID = detail.group?.group_id
  const [group, setGroup] = useState<HongGuoGroup | null>(null)
  const [loading, setLoading] = useState(Boolean(groupID))
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(detail.title)
  const [members, setMembers] = useState<HongGuoGroupInput[]>([{ source_id: detail.source_id, season_number: 1 }])
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  useEffect(() => {
    if (!groupID) return
    const controller = new AbortController()
    setLoading(true); setError(false)
    void hongguoAPI.group(groupID, controller.signal).then((value) => {
      if (controller.signal.aborted) return
      setGroup(value); setTitle(value.title); setMembers(value.members.map((m) => ({ source_id: m.source_id, season_number: m.season_number })))
    }).catch(() => { if (!controller.signal.aborted) setError(true) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [groupID, retry])
  const save = async () => {
    if (busy) return
    if (!title.trim() || !members.length || members.some((m) => !/^[1-9][0-9]{0,31}$/.test(m.source_id) || !Number.isInteger(m.season_number) || m.season_number < 1 || m.season_number > 1000) || new Set(members.map((m) => m.source_id)).size !== members.length || new Set(members.map((m) => m.season_number)).size !== members.length) {
      toast.error('请检查作品 ID 和季号，不能重复'); return
    }
    setBusy(true)
    try { await hongguoAPI.saveGroup(groupID, title.trim(), members); toast.success('聚合已保存'); onSaved() }
    catch { toast.error('保存失败：请先导入源作品，并确认不是电影或其他聚合的成员') }
    finally { setBusy(false) }
  }
  return <section className="space-y-3 border-t border-ink-100/10 pt-4">
    <h3 className="text-lg font-semibold">人工跨季聚合</h3>
    {loading ? <p role="status">读取聚合中…</p> : error ? <p role="alert">聚合读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : <>
      {group ? <><p>{group.title}</p><div className="flex flex-wrap gap-2">{group.members.map((m) => <button className="btn-outline" key={m.source_id} onClick={() => navigate({ id: m.source_id })}>第 {m.season_number} 季 · {m.title}</button>)}</div></> : <p className="text-sm text-ink-50">未聚合，作为独立剧的第一季展示。</p>}
      {admin && !editing && <button className="btn-outline" onClick={() => setEditing(true)}>{group ? '编辑聚合' : '建立聚合'}</button>}
      {admin && editing && <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void save() }}>
        <label className="block">聚合剧名<input className="input-field mt-1 w-full" required value={title} disabled={busy} onChange={(e) => setTitle(e.target.value)} /></label>
        {members.map((member, index) => <div className="flex flex-wrap items-end gap-2" key={index}>
          <label className="min-w-0 flex-1">源作品 ID<input className="input-field mt-1 w-full" required disabled={busy} value={member.source_id} onChange={(e) => setMembers((rows) => rows.map((m, i) => i === index ? { ...m, source_id: e.target.value.trim() } : m))} /></label>
          <label>呈现季号<input className="input-field mt-1 block w-24" type="number" min={1} max={1000} required disabled={busy} value={member.season_number} onChange={(e) => setMembers((rows) => rows.map((m, i) => i === index ? { ...m, season_number: Number(e.target.value) } : m))} /></label>
          <button className="btn-outline" type="button" disabled={busy || members.length === 1} onClick={() => setMembers((rows) => rows.filter((_, i) => i !== index))}>移除</button>
        </div>)}
        <p className="text-sm text-ink-50">文件仍使用各源作品 ID 与 S01Exxx；聚合不改文件，不改变观看进度。</p>
        <div className="flex flex-wrap gap-2"><button className="btn-outline" type="button" disabled={busy || members.length >= 1000} onClick={() => setMembers((rows) => [...rows, { source_id: '', season_number: Math.max(...rows.map((m) => m.season_number)) + 1 }])}>添加一季</button><button className="btn-primary" disabled={busy}>保存聚合</button><button className="btn-outline" type="button" disabled={busy} onClick={() => setEditing(false)}>取消编辑</button>{groupID && <button className="btn-danger" type="button" disabled={busy} onClick={() => setConfirmDelete(true)}>解除整个聚合</button>}</div>
      </form>}
    </>}
    {confirmDelete && groupID && <ConfirmDialog options={{ title: '解除聚合', message: '所有成员将恢复独立剧展示，资料、文件和观看进度均保留。', confirmText: '解除聚合' }} onClose={(confirmed) => {
      setConfirmDelete(false)
      if (!confirmed || busy) return
      setBusy(true)
      void hongguoAPI.deleteGroup(groupID).then(() => { toast.success('已解除聚合'); onSaved() }).catch(() => toast.error('解除失败')).finally(() => setBusy(false))
    }} />}
  </section>
}
