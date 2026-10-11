import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ArrowLeft, Film, RefreshCw, Search, Star, UserRound } from 'lucide-react'
import toast from 'react-hot-toast'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import { discoverAPI } from '../api/discover'
import { imageURL } from '../api/client'
import { tmdbCenterAPI as api, type TMDbCredit, type TMDbDetail, type TMDbPage, type TMDbPerson, type TMDbSearchRow, type TMDbSubject } from '../api/tmdbCenter'

const names: Record<string, string> = { movie: '电影', tv: '电视剧', series: '电视剧', season: '季', episode: '集', person: '人物' }
const qualities = ['身份记录', '摘要资料', '基础资料', '完整详情']
const date = (v?: string) => v && !v.startsWith('0001-') ? new Date(v).toLocaleString() : '时间未知'
const errorText = (e: unknown) => (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '读取失败，请重试'
type Selection = { id: string; person: boolean; title: string }

function Picture({ src, title, person = false, large = false }: { src?: string; title: string; person?: boolean; large?: boolean }) {
  const url = imageURL(src, undefined, { maxWidth: large ? 600 : 160 })
  const [failed, setFailed] = useState('')
  return <div className={`shrink-0 overflow-hidden bg-[var(--app-glass)] ${person ? 'aspect-square rounded-full' : 'aspect-[2/3] rounded-lg'} ${large ? 'w-28 sm:w-40' : 'w-12'}`}>
    {url && failed !== url ? <img src={url} alt={title} className="h-full w-full object-cover" loading="lazy" decoding="async" onError={() => setFailed(url)} /> : <div className="flex h-full items-center justify-center text-ink-50">{person ? <UserRound /> : <Film />}</div>}
  </div>
}
function Pager({ page, total, size = 30, setPage }: { page: number; total: number; size?: number; setPage: (n: number) => void }) {
  if (!total) return null
  return <div className="flex items-center justify-between gap-3 text-sm"><span>共 {total} 条 · 第 {page} 页</span><div className="flex gap-2"><button className="btn-outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button><button className="btn-outline" disabled={page * size >= total} onClick={() => setPage(page + 1)}>下一页</button></div></div>
}

export function TMDbDataCenterPage() {
  const [params, setParams] = useSearchParams()
  const kind = ['movie', 'tv', 'person'].includes(params.get('kind') || '') ? params.get('kind')! : 'movie'
  const keyword = (params.get('keyword') || '').trim().slice(0, 200)
  const quality = ['0', '1', '2', '3'].includes(params.get('quality') || '') ? params.get('quality')! : ''
  const state = ['pending', 'error'].includes(params.get('state') || '') ? params.get('state')! : ''
  const [owned, setOwned] = useState<number[]>([])
  const rawPage = Number(params.get('page')); const page = Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 1000000 ? rawPage : 1
  const [draft, setDraft] = useState(keyword)
  const [data, setData] = useState<TMDbPage<TMDbSubject | TMDbPerson> | null>(null)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const [add, setAdd] = useState(false)
  const trigger = useRef<HTMLElement | null>(null)
  const selected = params.get('person') ? { id: params.get('person')!, person: true, title: '' } : params.get('subject') ? { id: params.get('subject')!, person: false, title: '' } : null
  const change = (values: Record<string, string>) => { const next = new URLSearchParams(params); for (const [k, v] of Object.entries(values)) { if (v) next.set(k, v); else next.delete(k) }; setParams(next) }
  useEffect(() => {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries({ provider: 'tmdb', kind, keyword, quality, state, page: page === 1 ? '' : String(page) })) { if (v) next.set(k, v); else next.delete(k) }
    next.delete('type')
    for (const key of ['subject', 'person']) { const value = next.get(key); if (value && !/^[a-f0-9-]{36}$/i.test(value)) next.delete(key) }
    if (next.has('person')) next.delete('subject')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, kind, keyword, quality, state, page, setParams])
  useEffect(() => { setDraft(keyword) }, [keyword])
  useEffect(() => {
    const c = new AbortController(); setData(null); setError('')
    const query = { keyword, kind, page, state, ...(quality ? { quality } : {}) }
    const request = kind === 'person' ? api.people(query, c.signal) : api.subjects(query, c.signal)
    request.then(v => { if (!c.signal.aborted) setData(v) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) })
    return () => c.abort()
  }, [keyword, kind, page, quality, state, revision])
  useEffect(() => {
    setOwned([])
    if (!data || kind === 'person' || !data.data.length) return
    const c = new AbortController()
    discoverAPI.libraryStatus(data.data.map(v => ({tmdb_id:v.tmdb_id,media_type:kind as 'movie' | 'tv'})),c.signal).then(v => {if (!c.signal.aborted) setOwned(v.map(x => x.tmdb_id))}).catch(() => {})
    return () => c.abort()
  }, [data, kind])
  const open = (v: Selection) => { trigger.current = document.activeElement as HTMLElement; change({ subject: v.person ? '' : v.id, person: v.person ? v.id : '' }) }
  const close = () => { change({ subject: '', person: '' }); requestAnimationFrame(() => trigger.current?.focus()) }
  return <div className="space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="font-display text-xl font-semibold">TMDb 资料中心</h1><p className="mt-1 text-sm text-ink-50">已保存的来源资料 · 与本地媒体独立维护</p></div><button className="btn-outline" onClick={() => setAdd(true)}>搜索 / 添加资料</button></div>
    <div className="flex flex-wrap gap-2">{['movie', 'tv', 'person'].map(k => <button key={k} className={kind === k ? 'btn-primary' : 'btn-outline'} aria-pressed={kind === k} onClick={() => change({ kind: k, page: '', subject: '', person: '', quality: '' })}>{names[k]}</button>)}</div>
    <form className="flex flex-wrap gap-2" onSubmit={e => { e.preventDefault(); change({ keyword: draft.trim(), page: '' }) }}><input className="input-field min-w-0 flex-1" aria-label="筛选已保存资料" placeholder="筛选标题、姓名或 TMDb ID" value={draft} onChange={e => setDraft(e.target.value)} maxLength={200} /><button className="btn-outline" aria-label="筛选"><Search size={18} /></button>{<Select className="input-field w-36" value={quality} onChange={v => change({ quality: v, page: '' })}><option value="">全部资料级别</option>{qualities.map((v, i) => <option key={i} value={String(i)}>{v}</option>)}</Select>}<Select aria-label="刷新状态" className="input-field w-32" value={state} onChange={v => change({state:v,page:''})}><option value="">全部状态</option><option value="pending">待补全</option><option value="error">刷新失败</option></Select></form>
    {error && <p role="alert">{error} <button className="btn-outline" onClick={() => setRevision(v => v + 1)}>重试</button></p>}
    {!data && !error && <p role="status" className="text-ink-50">正在读取资料…</p>}
    {data && <><div className="divide-y divide-[var(--app-border)] rounded-xl border border-[var(--app-border)]">{data.data.map(v => { const person = 'person_id' in v; const title = person ? v.name : v.title; return <button key={v.id} className="flex w-full items-center gap-4 p-4 text-left hover:bg-[var(--app-glass)]" onClick={() => open({ id: person ? v.person_id : v.id, person, title })}><Picture src={person ? v.profile_url : v.poster_url} title={title} person={person} /><div className="min-w-0 flex-1"><p className="truncate font-semibold">{title || `TMDb ${v.tmdb_id}`}</p><p className="mt-1 truncate text-sm text-ink-50">{v.original_name} · {person ? v.department : v.release_date || '日期未知'}</p><p className="mt-1 text-xs text-ink-50">{qualities[v.quality]} · {date(v.fetched_at)}</p></div>{!person && owned.includes(v.tmdb_id) && <span className="badge-neutral">已有本地媒体</span>}{!person && v.rating != null && <span className="flex items-center gap-1 text-amber-500"><Star size={14} />{v.rating.toFixed(1)}</span>}</button> })}{!data.data.length && <p className="p-8 text-center text-ink-50">暂无符合条件的已保存资料</p>}</div><Pager page={page} total={data.total} setPage={n => change({ page: String(n) })} /></>}
    {selected && <TMDbDetailModal key={`${selected.person}-${selected.id}`} initial={selected} onClose={close} onChanged={() => setRevision(v => v + 1)} />}
    {add && <AddTMDb kind={kind} onClose={() => setAdd(false)} onAdded={v => { setAdd(false); setRevision(n => n + 1); open(v) }} />}
  </div>
}

function TMDbDetailModal({ initial, onClose, onChanged }: { initial: Selection; onClose: () => void; onChanged: () => void }) {
  const [history, setHistory] = useState([initial]); const selected = history[history.length - 1]
  return <ModalShell onClose={onClose} ariaLabel="TMDb 来源资料" maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto p-5 sm:p-8">
    {history.length > 1 && <button className="btn-outline mb-5" onClick={() => setHistory(v => v.slice(0, -1))}><ArrowLeft size={16} />返回 {history[history.length - 2].title || '上一层'}</button>}
    <TMDbDetailContent key={`${selected.person}-${selected.id}`} selected={selected} onOpen={v => setHistory(h => [...h.map((x, i) => i === h.length - 1 ? { ...x, title: x.title || selected.title } : x), v])} onChanged={onChanged} />
  </ModalShell>
}

function TMDbDetailContent({ selected, onOpen, onChanged }: { selected: Selection; onOpen: (v: Selection) => void; onChanged: () => void }) {
  const [detail, setDetail] = useState<TMDbDetail | null>(null); const [error, setError] = useState(''); const [revision, setRevision] = useState(0)
  const [pending, setPending] = useState(false); const [works, setWorks] = useState(!selected.person)
  const [children, setChildren] = useState<TMDbPage<TMDbSubject> | null>(null); const [childPage, setChildPage] = useState(1)
  const [credits, setCredits] = useState<TMDbPage<TMDbCredit> | null>(null); const [creditPage, setCreditPage] = useState(1)
  const [workKind, setWorkKind] = useState(''); const [workCategory, setWorkCategory] = useState(''); const [workOrder, setWorkOrder] = useState('newest')
  const [imageRevision, setImageRevision] = useState(0)
  const mutation = useRef<AbortController | null>(null)
  const ensured = useRef(false)
  useEffect(() => () => mutation.current?.abort(), [])
  useEffect(() => {
    const c = new AbortController(); let timer: ReturnType<typeof setTimeout> | undefined
    const load = () => api.detail(selected.id, selected.person, c.signal).then(v => { if (c.signal.aborted) return; setDetail(v); setError(''); if (v.jobs.some(j => ['pending', 'running'].includes(j.status))) timer = setTimeout(load, 2000) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) })
    void load(); return () => { c.abort(); clearTimeout(timer) }
  }, [selected.id, selected.person, revision])
  useEffect(() => {
    if (!detail || ensured.current || !detail.missing_scopes.includes('detail') || detail.subject?.kind === 'episode') return
    ensured.current = true
    const c = new AbortController(); mutation.current = c
    api.ensure(selected.id, selected.person, 'detail', c.signal).then(() => { if (!c.signal.aborted) setRevision(v => v + 1) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) })
  }, [detail, selected.id, selected.person])
  const subject = detail?.subject; const person = detail?.person; const kind = subject?.kind
  const stamp = detail?.components.map(v => v.fetched_at).join(',')
  useEffect(() => {
    if (!kind || !['series', 'season'].includes(kind)) return
    const c = new AbortController(); setChildren(null); api.children(selected.id, kind, childPage, c.signal).then(v => { if (!c.signal.aborted) setChildren(v) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) }); return () => c.abort()
  }, [kind, selected.id, childPage, stamp])
  useEffect(() => {
    if (!works) return
    const c = new AbortController(); setCredits(null); api.credits(selected.id, selected.person, creditPage, c.signal, {kind: workKind, category: workCategory, order: workOrder}).then(v => { if (!c.signal.aborted) setCredits(v) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) }); return () => c.abort()
  }, [selected.id, selected.person, works, creditPage, stamp, workKind, workCategory, workOrder])
  const refresh = async (scope: string) => { if (pending) return; const c = new AbortController(); mutation.current = c; setPending(true); setError(''); try { await api.refresh(selected.id, selected.person, scope, c.signal); if (!c.signal.aborted) { setRevision(v => v + 1); onChanged() } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  const retryImage = async () => { const c = new AbortController(); mutation.current = c; setPending(true); try { await api.retryImage(selected.person ? 'person' : kind!, (detail?.person || detail?.subject)!.tmdb_id, selected.person ? 'profile' : kind === 'episode' ? 'still' : 'poster', c.signal); if (!c.signal.aborted) { setImageRevision(v => v + 1); setRevision(v => v + 1) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  if (!detail) return <p role={error ? 'alert' : 'status'}>{error || '正在读取来源资料…'}{error && <button className="btn-outline" onClick={() => setRevision(v => v + 1)}>重试</button>}</p>
  const title = person?.name || subject?.title || '来源资料'; const fields = person?.fields || subject?.fields || {}
  const busy = pending || detail.jobs.some(j => ['pending', 'running'].includes(j.status))
  const imdb = fields.external_ids?.imdb_id || fields.imdb_id
  return <div className="space-y-7">
    {!!detail.parents?.length && <nav aria-label="资料层级" className="flex flex-wrap items-center gap-2 text-sm">{detail.parents.map(p => <span key={p.id}><button className="text-accent-400 hover:underline" onClick={() => onOpen({id:p.id,person:false,title:p.title})}>{p.title || (p.kind === 'series' ? '电视剧' : `第 ${p.number} 季`)}</button> / </span>)}<span>{title}</span></nav>}
    {subject?.backdrop_url && ['movie', 'series'].includes(subject.kind) && <img src={imageURL(subject.backdrop_url, undefined, {maxWidth:1280})} alt="" className="max-h-64 w-full rounded-xl object-cover" onError={e => { e.currentTarget.style.display = 'none' }} />}
    <div className="flex gap-5"><div className="space-y-2"><Picture src={`${person?.profile_url || subject?.poster_url}&retry=${imageRevision}`} title={title} person={selected.person} large /><button disabled={busy} className="btn-outline w-full text-xs" onClick={() => void retryImage()}>重试图片</button></div><div className="min-w-0 flex-1"><p className="text-xs tracking-widest text-ink-50">TMDb · {person ? '人物' : names[kind || 'movie']}</p><h2 className="mt-2 font-display text-2xl font-semibold sm:text-3xl">{title}</h2><p className="mt-2 text-sm text-ink-50">{person?.original_name || subject?.original_name}</p>{subject && <div className="mt-3 flex flex-wrap items-center gap-3 text-sm"><span>{subject.release_date || '日期未知'}</span><span>{subject.status}</span>{subject.rating != null && <span className="text-amber-500">★ {subject.rating.toFixed(1)} · {subject.vote_count} 人评分</span>}{fields.runtime ? <span>{fields.runtime} 分钟</span> : null}</div>}{person && <p className="mt-3 text-sm">{person.department} · {fields.birthday || '生日未知'}{fields.deathday ? ` — ${fields.deathday}` : ''}<br />{fields.place_of_birth}</p>}
      <div className="mt-4 flex flex-wrap gap-2"><button disabled={busy} className="btn-outline" onClick={() => void refresh('detail')}><RefreshCw size={15} className={busy ? 'animate-spin' : ''} />{busy ? '正在更新…' : '刷新来源资料'}</button>{subject && subject.kind !== 'episode' && <button disabled={busy} className="btn-outline" onClick={() => void refresh('base')}>仅基础资料</button>}<a className="btn-outline" target="_blank" rel="noreferrer" href={detail.source_url}>TMDb ↗</a>{imdb && (person ? /^nm\d+$/ : /^tt\d+$/).test(imdb) && <a className="btn-outline" href={`https://www.imdb.com/${person ? 'name' : 'title'}/${imdb}/`} target="_blank" rel="noreferrer">IMDb ↗</a>}</div>
    </div></div>
    {error && <p role="alert" className="text-red-500">{error}</p>}
    {detail.missing_scopes.includes('detail') && !busy && <p className="rounded-lg bg-[var(--app-glass)] p-3 text-sm text-ink-50">尚未获取完整扩展，可点击“刷新来源资料”补齐这一项。</p>}
    <div className="flex flex-wrap gap-2">{fields.genres?.map(v => <span className="badge-neutral" key={v.id}>{v.name}</span>)}{fields.production_countries?.map(v => <span className="badge-neutral" key={v.iso_3166_1}>{v.name}</span>)}{fields.spoken_languages?.map(v => <span className="badge-neutral" key={v.iso_639_1}>{v.name}</span>)}</div>
    {subject?.kind === 'series' && <p className="text-sm text-ink-50">{fields.number_of_seasons != null && `${fields.number_of_seasons} 季 · `}{fields.number_of_episodes != null && `${fields.number_of_episodes} 集 · `}最近播出：{fields.last_air_date || '未知'}{!!fields.episode_run_time?.length && ` · 每集 ${fields.episode_run_time.join(' / ')} 分钟`}</p>}
    <p className="whitespace-pre-line text-sm leading-7 text-ink-400">{person?.biography || subject?.overview || '暂无简介'}</p>
    {!!fields.also_known_as?.length && <p className="text-sm text-ink-50">别名：{fields.also_known_as.join(' / ')}</p>}
    {children && <section className="space-y-4"><h3 className="font-semibold">{kind === 'series' ? '季' : '集'} · {children.total}</h3><div className="grid gap-3 sm:grid-cols-2">{children.data.map(v => <button key={v.id} className="flex items-center gap-3 rounded-lg border border-[var(--app-border)] p-3 text-left" onClick={() => onOpen({ id: v.id, person: false, title: v.title })}><Picture src={v.poster_url} title={v.title} /><div className="min-w-0"><p className="font-medium">{v.number === 0 ? '特别篇' : `第 ${v.number} ${v.kind === 'season' ? '季' : '集'}`} · {v.title}</p><p className="mt-1 text-xs text-ink-50">{v.release_date} · {qualities[v.quality]}</p></div></button>)}</div><Pager page={childPage} total={children.total} setPage={setChildPage} /></section>}
    {person && <button className="btn-outline" disabled={busy} onClick={() => { setWorks(true); if (works || detail.missing_scopes.includes('filmography')) void refresh('filmography') }}>{works ? '刷新作品履历' : '查看作品履历'}</button>}
    {person && works && <div className="flex flex-wrap gap-2"><Select aria-label="作品类型" className="input-field w-32" value={workKind} onChange={v => {setWorkKind(v);setCreditPage(1)}}><option value="">全部作品</option><option value="movie">电影</option><option value="tv">电视剧</option></Select><Select aria-label="参演或幕后" className="input-field w-32" value={workCategory} onChange={v => {setWorkCategory(v);setCreditPage(1)}}><option value="">全部职务</option><option value="cast">参演</option><option value="crew">幕后</option></Select><Select aria-label="作品年份排序" className="input-field w-32" value={workOrder} onChange={v => {setWorkOrder(v);setCreditPage(1)}}><option value="newest">最新优先</option><option value="oldest">最早优先</option></Select></div>}
    {works && credits && <section className="space-y-4"><h3 className="font-semibold">{person ? '参演与幕后作品' : kind === 'episode' ? '本集客串与工作人员' : '演职员'}</h3>{!credits.data.length && <p className="text-sm text-ink-50">暂无已保存关系</p>}<div className={person ? 'grid gap-3 sm:grid-cols-2' : 'flex flex-wrap gap-4'}>{credits.data.map(v => person ? <button key={v.id} className="flex gap-3 rounded-lg border border-[var(--app-border)] p-3 text-left" onClick={() => onOpen({ id: v.subject.id, person: false, title: v.subject.title })}><Picture src={v.subject.poster_url} title={v.subject.title} /><div><p>{v.subject.title}</p><p className="mt-1 text-xs text-ink-50">{v.subject.release_date} · {[v.character, v.job].filter(Boolean).join(' / ')}</p></div></button> : <button key={v.id} className="flex w-24 flex-col items-center gap-2 text-center" onClick={() => onOpen({ id: v.person.person_id, person: true, title: v.person.name })}><Picture src={v.person.profile_url} title={v.person.name} person /><span className="text-sm">{v.person.name}</span><span className="text-xs text-ink-50">{[v.character, v.job].filter(Boolean).join(' / ')}</span></button>)}</div><Pager page={creditPage} total={credits.total} size={60} setPage={setCreditPage} /></section>}
    <details className="rounded-lg border border-[var(--app-border)] p-4 text-sm"><summary className="cursor-pointer">来源记录与刷新状态</summary><div className="mt-4 space-y-3">{detail.jobs.map(j => <p key={j.id}>{j.scope} · {j.language} · {j.status} · 上次成功 {date(j.last_success_at)}{j.error && ` · ${j.error}`}</p>)}{detail.components.map(v => <details key={`${v.component}-${v.language}`}><summary className="cursor-pointer">{v.component} · {v.language} · {date(v.fetched_at)}</summary><pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded bg-[var(--app-glass)] p-3 text-xs">{JSON.stringify(v.raw_json, null, 2)}</pre></details>)}</div></details>
  </div>
}

function AddTMDb({ kind, onClose, onAdded }: { kind: string; onClose: () => void; onAdded: (v: Selection) => void }) {
  const [query, setQuery] = useState(''); const [id, setID] = useState(''); const [rows, setRows] = useState<TMDbSearchRow[]>([]); const [page, setPage] = useState(1); const [pages, setPages] = useState(0); const [pending, setPending] = useState(false); const [error, setError] = useState('')
  const ref = useRef<AbortController | null>(null); useEffect(() => () => ref.current?.abort(), [])
  const search = async (p: number) => { ref.current?.abort(); const c = new AbortController(); ref.current = c; setPending(true); setError(''); try { const v = await api.search(kind, query.trim(), p, c.signal); if (!c.signal.aborted) { setRows(v.results); setPages(v.total_pages); setPage(p) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  const add = async (tmdbID: number) => { if (pending) return; const c = new AbortController(); ref.current = c; setPending(true); setError(''); try { const v = await api.import(kind, tmdbID, c.signal); if (!c.signal.aborted) { toast.success('已登记，正在补充来源资料'); onAdded({ id: v.id, person: kind === 'person', title: '' }) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  return <ModalShell onClose={onClose} ariaLabel="添加 TMDb 资料" maxWidth="max-w-xl" className="max-h-[85vh] overflow-y-auto p-6"><h2 className="text-lg font-semibold">添加{names[kind]}资料</h2><form className="mt-4 flex gap-2" onSubmit={e => { e.preventDefault(); void add(Number(id)) }}><input className="input-field min-w-0 flex-1" aria-label="TMDb ID" placeholder="输入 TMDb ID" inputMode="numeric" value={id} onChange={e => setID(e.target.value)} /><button className="btn-primary" disabled={pending || !/^[1-9]\d*$/.test(id)}>按 ID 添加</button></form><form className="mt-4 flex gap-2" onSubmit={e => { e.preventDefault(); void search(1) }}><input className="input-field min-w-0 flex-1" aria-label="搜索 TMDb" placeholder="输入标题或姓名" value={query} maxLength={100} onChange={e => setQuery(e.target.value)} /><button className="btn-outline" disabled={pending || !query.trim()}>搜索 TMDb</button></form>{error && <p role="alert" className="mt-3 text-red-500">{error}</p>}<div className="mt-4 divide-y divide-[var(--app-border)]">{rows.map(v => <button disabled={pending} key={v.id} className="block w-full py-3 text-left" onClick={() => void add(v.id)}>{v.title || v.name}<span className="ml-2 text-xs text-ink-50">{v.release_date || v.first_air_date} · {v.id}</span></button>)}</div>{pages > 1 && <Pager page={page} size={20} total={Math.min(pages, 500) * 20} setPage={p => void search(p)} />}</ModalShell>
}
