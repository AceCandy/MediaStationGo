import { useCallback, useEffect, useRef, useState } from 'react'
import { Film, RefreshCw, ExternalLink, Star } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { doubanAPI, type DoubanRefreshMode, type DoubanSubjectDetail, type DoubanSubjectSummary } from '../api/douban'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'
import { CatalogLoadMore } from '../components/CatalogLoadMore'
import { useCatalogContinuation } from '../hooks/useCatalogContinuation'
import { CatalogSearchForm } from '../components/CatalogSearchForm'
import { CatalogFilters } from '../components/CatalogFilters'
import { CatalogPosterCard } from '../components/CatalogPosterCard'
import { useAuthStore } from '../stores/auth'
import { CatalogCredits } from './CatalogCredits'

const kinds: Record<string, string> = { movie: '电影', series: '电视剧' }
const qualityLabels: Record<string, string> = { complete: '完整资料', degraded: '降级资料', partial: '摘要资料' }
const timeText = (value: string | null) => value ? new Date(value).toLocaleString() : '—'
const ratingText = (value: number) => value > 0 ? value.toFixed(1) : '—'
const errorText = (error: unknown, fallback: string) => (error as { response?: { data?: { message?: string } } })?.response?.data?.message || fallback

function Quality({ subject }: { subject: DoubanSubjectSummary }) {
  return <span className={subject.quality === 'complete' ? 'badge-sage' : 'badge-neutral'}>{subject.has_payload ? qualityLabels[subject.quality] || '已有资料' : '未获取资料'}</span>
}

function SubjectPoster({ subject, large = false }: { subject: DoubanSubjectSummary; large?: boolean }) {
  const src = imageURL(subject.poster_url, subject.fetched_at || undefined, { maxWidth: large ? 600 : 160 })
  const [failedURL, setFailedURL] = useState('')
  return <div className={`flex aspect-[2/3] shrink-0 items-center justify-center overflow-hidden rounded bg-[var(--app-glass)] ${large ? 'w-28 sm:w-40' : 'w-12'}`}>
    {src && failedURL !== src ? <img src={src} alt={`${subject.title || '豆瓣资料'}海报`} className="h-full w-full object-cover" loading="lazy" decoding="async" onError={() => setFailedURL(src)} /> : <Film aria-label="暂无海报" className="text-ink-50" size={large ? 40 : 20} />}
  </div>
}

export function DoubanDataCenterPage() {
  const sessionVersion = useAuthStore((state) => state.sessionVersion)
  return <DoubanDataCenterContent key={sessionVersion} />
}

function DoubanDataCenterContent() {
  const [params, setParams] = useSearchParams()
  const keyword = Array.from((params.get('keyword') || '').trim()).slice(0, 200).join('')
  const type = Object.keys(kinds).includes(params.get('type') || '') ? params.get('type')! : 'movie'
  const genre = (params.get('genre') || '').slice(0, 100)
  const [categories, setCategories] = useState<{ value: string; label: string }[]>([])
  const [categoryError, setCategoryError] = useState(false)
  const [categoryRevision, setCategoryRevision] = useState(0)
  useEffect(() => { const c = new AbortController(); setCategories([]); setCategoryError(false); doubanAPI.categories(type, c.signal).then(v => { if (!c.signal.aborted) setCategories(v) }).catch(() => { if (!c.signal.aborted) setCategoryError(true) }); return () => c.abort() }, [type, categoryRevision])
  const [draft, setDraft] = useState(keyword)
  const [revision, setRevision] = useState(0)
  const subjectID = params.get('subject') || ''
  const selectedID = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(subjectID) ? subjectID : ''
  const detailTrigger = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const next = new URLSearchParams(params)
    for (const [key, value] of [['keyword', keyword], ['type', type], ['page', '']]) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    if (selectedID) next.set('subject', selectedID); else next.delete('subject')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [keyword, params, setParams, type, selectedID])
  useEffect(() => { setDraft(keyword) }, [keyword])
  const fetchPage = useCallback((page: number, signal: AbortSignal) => doubanAPI.list(keyword, type, page, signal, genre), [keyword, type, genre])
  const { items: rows, total, loading, error, hasMore, loadMore, sentinel } = useCatalogContinuation(fetchPage, revision, !!selectedID)

  const updateParams = (values: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(values)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    next.delete('subject')
    setParams(next)
  }
  const openDetail = (subject: DoubanSubjectSummary) => {
    detailTrigger.current = document.activeElement as HTMLElement
    const next = new URLSearchParams(params); next.set('subject', subject.id); setParams(next)
  }
  const closeDetail = () => {
    const next = new URLSearchParams(params); next.delete('subject'); setParams(next)
    requestAnimationFrame(() => detailTrigger.current?.focus())
  }
  const selected = selectedID ? rows.find(row => row.id === selectedID) || { id: selectedID, douban_id: '', entity_kind: 'movie' as const, title: '', year: '', rating: 0, quality: '', has_payload: false, poster_url: '', fetched_at: null, last_attempt_at: null, next_retry_at: null } : null

  return <div className="space-y-5">
    <CatalogFilters label="豆瓣类型" options={Object.entries(kinds).map(([value, label]) => ({value, label}))} value={type} onChange={value => updateParams({type: value, genre: '', page: ''})} />
    <CatalogFilters label="题材" options={[{value: '', label: '全部'}, ...categories]} value={genre} onChange={value => updateParams({genre: value, page: ''})} />
    {categoryError && <p role="alert" className="text-sm text-ink-50">分类选项读取失败，可继续搜索资料。<button className="btn-ghost" onClick={() => setCategoryRevision(v => v + 1)}>重试分类</button></p>}
    <CatalogSearchForm label="名称或豆瓣 ID" placeholder="搜索已有资料…" value={draft} onChange={setDraft} loading={loading} refreshLabel="重新读取列表" onSearch={() => updateParams({keyword:draft.trim(),page:''})} onRefresh={() => setRevision(v => v + 1)} />
    <section className="space-y-4" aria-label="豆瓣资料列表">
      {loading && !rows.length && <p role="status" className="p-10 text-center text-ink-50">正在读取资料…</p>}
      {error && <div className="space-y-3 p-8 text-center"><p role="alert" className="text-red-500">{error}</p><button type="button" className="btn-outline" onClick={loadMore}>重试读取</button></div>}
      {!loading && !error && !rows.length && <p className="p-10 text-center text-ink-50">没有符合条件的豆瓣资料</p>}
      {!!rows.length && <>
        <p className="text-sm text-ink-50">已显示 {rows.length} / {total}</p>
        <div className="grid grid-cols-2 gap-4 min-[480px]:grid-cols-3 sm:grid-cols-4 md:grid-cols-5">{rows.map(subject => <article key={subject.id} className="min-w-0 space-y-3"><CatalogPosterCard title={subject.title || `豆瓣 ${subject.douban_id}`} image={subject.poster_url} version={subject.fetched_at || undefined} year={subject.year} rating={subject.rating} onClick={() => openDetail(subject)} /></article>)}</div>
      </>}
    </section>
    <div ref={sentinel} aria-hidden="true" />
    <CatalogLoadMore loading={loading} hasMore={hasMore} hasItems={rows.length > 0} failed={!!error} onLoadMore={loadMore} endLabel="资料已加载完" />
    {selected && <DoubanSubjectDetails key={selected.id} subject={selected} onClose={closeDetail} onChanged={() => setRevision(v => v + 1)} />}
  </div>
}

export function DoubanSubjectDetails({ subject, onClose, onChanged }: { subject: DoubanSubjectSummary; onClose: () => void; onChanged?: () => void }) {
  const isAdmin = useAuthStore(s => s.user?.role === 'admin')
  const [revision, setRevision] = useState(0)
  const [pending, setPending] = useState(false)
  const [actionError, setActionError] = useState('')
  const mutation = useRef<AbortController | null>(null)
  useEffect(() => () => mutation.current?.abort(), [])
  const refresh = async (mode: DoubanRefreshMode) => {
    if (!isAdmin || mutation.current) return
    const controller = new AbortController()
    mutation.current = controller
    setPending(true)
    setActionError('')
    try {
      const result = await doubanAPI.refresh(subject.id, mode, controller.signal)
      if (controller.signal.aborted) return
      if (result.degraded) {
        toast(result.subject.quality === 'complete' ? '完整接口受限，已保留已有完整资料' : '完整接口受限，已保存降级资料')
      } else if (result.supplement_unavailable) toast('基础资料已更新，网页补充暂不可用，已有内容已保留')
      else toast.success(mode === 'base' ? '基础资料已更新，补充资料保持原样' : '基础与补充资料已更新')
    } catch (err) {
      if (!controller.signal.aborted) setActionError(errorText(err, '豆瓣资料更新失败，已有数据已保留'))
    } finally {
      if (!controller.signal.aborted) {
        mutation.current = null
        setPending(false)
        setRevision((value) => value + 1)
        onChanged?.()
      }
    }
  }
  const [detail, setDetail] = useState<DoubanSubjectDetail | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const [loading, setLoading] = useState(true)
  const [pollPaused, setPollPaused] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let reads = 0
    setPollPaused(false)
    setLoading(true)
    setError('')
    const load = () => doubanAPI.detail(subject.id, controller.signal).then((value) => {
      if (controller.signal.aborted) return
      setDetail(value)
      if (value.refresh_pending && ++reads < 45) timer = setTimeout(load, 2000)
      else if (value.refresh_pending) setPollPaused(true)
    }).catch((err) => { if (!controller.signal.aborted) setError(errorText(err, '详情读取失败')) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [subject.id, revision, retry])
  const item = detail || subject
  const facts = detail ? [
    ['导演', detail.directors?.join(' / ')], ['编剧', detail.writers?.join(' / ')],
    ['主演', detail.actors?.join(' / ')], ['类型', detail.genres?.join(' / ')],
    ['制片地区', detail.countries?.join(' / ')], ['语言', detail.languages?.join(' / ')],
    ['上映日期', detail.release_dates?.join(' / ') || detail.release_date],
    ['片长', detail.durations?.join(' / ')], ['又名', detail.aliases?.join(' / ')],
  ].filter(([, value]) => value) : []
  const shares = detail?.rating_distribution || []
  return <ModalShell onClose={onClose} ariaLabel={item.title || '豆瓣资料'} maxWidth="max-w-5xl" className="max-h-[90dvh] overflow-y-auto">
    <div className="space-y-6 p-5 sm:p-8">
      {detail?.refresh_pending && <p role="status" className="text-sm text-ink-100">正在补全来源资料和演职员…</p>}
      {pollPaused && <p className="text-sm text-ink-100">资料仍在排队，后台会继续处理。<button className="btn-outline ml-2" onClick={() => setRetry(value => value + 1)}>重新读取</button></p>}
      {detail?.source_error && <p role="status" className="text-sm text-ink-100">来源暂不可用，已有资料已保留，后台将按退避规则重试。</p>}
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 flex-1 basis-60"><h2 className="break-words font-display text-2xl font-semibold text-ink-600">{item.title || '未获取标题'}{item.year && <span className="ml-2 font-normal text-ink-50">({item.year})</span>}</h2><p className="mt-2 break-words text-xs text-ink-50">{detail?.original_name && `${detail.original_name} · `}{kinds[item.entity_kind]} · 豆瓣 {item.douban_id}</p></div>
        <div className="flex flex-wrap items-start gap-2"><a className="btn-outline gap-1.5 px-3 py-2 text-xs" href={`https://movie.douban.com/subject/${encodeURIComponent(item.douban_id)}/`} target="_blank" rel="noopener noreferrer">豆瓣原页<ExternalLink size={13} /></a>{isAdmin && <details className="relative"><summary className="btn-outline cursor-pointer">资料管理</summary><div className="mt-2 flex flex-wrap gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)] p-3"><RefreshButtons pending={pending} onRefresh={mode => void refresh(mode)} /></div></details>}</div>
      </header>
      {loading && <p className="text-sm text-ink-50">正在读取详情…</p>}
      {error && <div className="flex flex-wrap items-center gap-3"><p role="alert" className="text-sm text-red-500">{error}</p><button type="button" className="btn-outline px-3 py-2 text-xs" onClick={() => setRetry((value) => value + 1)}>重试读取详情</button></div>}
      {actionError && <p role="alert" className="text-sm text-red-500">{actionError}</p>}
      <div className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-5 lg:grid-cols-[auto_minmax(0,1fr)_12rem] lg:gap-7">
        <SubjectPoster subject={item} large />
        <dl className="min-w-0 space-y-2 text-xs leading-relaxed sm:text-sm">{facts.map(([label, value]) => <div className="sm:flex sm:gap-3" key={label}><dt className="shrink-0 whitespace-nowrap text-ink-50 sm:w-20">{label}</dt><dd className="min-w-0 break-words text-ink-600">{value}</dd></div>)}{detail?.imdb_id && <div className="sm:flex sm:gap-3"><dt className="shrink-0 whitespace-nowrap text-ink-50 sm:w-20">IMDb</dt><dd><a href={`https://www.imdb.com/title/${encodeURIComponent(detail.imdb_id)}/`} className="text-brand-500 hover:underline" target="_blank" rel="noopener noreferrer">{detail.imdb_id} ↗</a></dd></div>}</dl>
        <aside className="col-span-2 border-t border-[var(--app-border)] pt-5 lg:col-span-1 lg:border-l lg:border-t-0 lg:pl-6 lg:pt-0" aria-label="豆瓣评分"><p className="text-xs text-ink-50">豆瓣评分</p><div className="mt-2 flex flex-wrap items-center gap-4 lg:block"><p className="text-4xl font-semibold text-ink-600">{ratingText(item.rating)}{item.rating > 0 && <span className="ml-2 text-xs font-normal text-ink-50">/ 10</span>}</p><div><div className="my-2 flex gap-0.5 text-gold-500" aria-hidden="true">{Array.from({ length: 5 }, (_, i) => <Star key={i} size={16} fill={item.rating >= (i + 1) * 2 ? 'currentColor' : 'none'} />)}</div>{!!detail?.rating_count && <p className="text-xs text-ink-50">{detail.rating_count.toLocaleString()} 人评价</p>}</div></div>{shares.length > 0 && <div className="mt-4 max-w-xs space-y-1.5" aria-label="评分星级分布">{shares.map((share) => <div key={share.stars} className="flex items-center gap-2 text-[11px] text-ink-50"><span>{share.stars}星</span><div className="h-1.5 flex-1 overflow-hidden rounded bg-[var(--app-glass)]"><div className="h-full rounded bg-gold-500" style={{ width: `${share.percent}%` }} /></div><span className="w-10 text-right">{share.percent}%</span></div>)}</div>}{detail?.honors?.find((honor) => honor.rank > 0) && <p className="mt-5 border-t border-[var(--app-border)] pt-3 text-xs text-gold-500">{detail.honors.find((honor) => honor.rank > 0)!.title} · 第 {detail.honors.find((honor) => honor.rank > 0)!.rank} 名</p>}</aside>
      </div>
      {detail && <>
        {detail.overview && <section className="border-t border-[var(--app-border)] pt-5"><h3 className="mb-3 font-display text-base font-semibold text-ink-600">剧情简介</h3><p className="whitespace-pre-wrap break-words text-sm leading-7 text-ink-100">{detail.overview}</p></section>}
        {!!detail.credits?.length && <CatalogCredits key={`${item.id}:${detail.rich_fetched_at || ''}`} credits={detail.credits} />}
        {!!detail.awards?.length && <section><h3 className="mb-3 font-display text-base font-semibold text-ink-600">获奖与提名</h3><div className="divide-y divide-[var(--app-border)]">{detail.awards.map((award, index) => <div key={index} className="grid gap-1 py-3 text-xs sm:grid-cols-[14rem_minmax(0,1fr)] sm:gap-4"><p className="text-brand-500">{award.event}</p><p className="break-words text-ink-100">{award.category}{award.people && <span className="ml-2 text-ink-50">{award.people}</span>}</p></div>)}</div></section>}
        {!!detail.honors?.length && <section><h3 className="mb-3 font-display text-base font-semibold text-ink-600">榜单与荣誉</h3><div className="flex flex-wrap gap-2">{detail.honors.map((honor) => <span key={honor.title} className="rounded-lg border border-[var(--app-border)] bg-[var(--app-glass)] px-3 py-2 text-xs text-ink-100">{honor.rank > 0 && <strong className="mr-2 text-gold-500">第 {honor.rank} 名</strong>}{honor.title}</span>)}</div></section>}
      </>}
      {isAdmin && <section className="border-t border-[var(--app-border)] pt-5"><div className="mb-3 flex items-center gap-3"><h3 className="text-sm font-medium text-ink-600">资料状态</h3><Quality subject={item} /></div><dl className="grid gap-3 text-xs sm:grid-cols-2">{[['基础资料更新', timeText(item.fetched_at)], ['补充资料更新', timeText(item.rich_fetched_at || null)], ['最近尝试', timeText(item.last_attempt_at)], ['下次可重试', timeText(item.next_retry_at)]].map(([label, value]) => <div key={label}><dt className="text-ink-50">{label}</dt><dd className="mt-1 text-ink-100">{value}</dd></div>)}</dl></section>}
      {isAdmin && detail && (detail.has_payload ? <details className="border-t border-[var(--app-border)] pt-4"><summary className="cursor-pointer text-xs text-ink-50">原始豆瓣资料</summary><pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs text-ink-100">{JSON.stringify(detail.raw_json, null, 2)}</pre></details> : <p className="text-sm text-ink-50">该记录尚未成功获取资料。</p>)}
    </div>
  </ModalShell>
}

function RefreshButtons({ pending, onRefresh, subjectID }: { pending: boolean; onRefresh: (mode: DoubanRefreshMode) => void; subjectID?: string }) {
  const isAdmin = useAuthStore(s => s.user?.role === 'admin')
  if (!isAdmin) return null
  return <>{([['base', '基础更新'], ['full', '完整更新']] as const).map(([mode, label]) => <button key={mode} type="button" className="btn-outline gap-1.5 px-3 py-2 text-xs" aria-label={`${label}豆瓣${subjectID ? ` ${subjectID}` : '资料'}`} title={mode === 'base' ? '只请求基础接口，更新资料和评分' : '更新基础资料，并尝试获取网页补充'} disabled={pending} onClick={() => onRefresh(mode)}><RefreshCw size={14} />{label}</button>)}</>
}
