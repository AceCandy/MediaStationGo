import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { ModalShell } from '../components/ModalShell'
import { CatalogPager as Pager } from '../components/CatalogPager'
import { CatalogSearchForm } from '../components/CatalogSearchForm'
import { Select } from '../components/Select'
import { CatalogPosterCard } from '../components/CatalogPosterCard'
import { useAuthStore } from '../stores/auth'
import { discoverAPI } from '../api/discover'
import { tmdbCenterAPI as api, type TMDbPerson, type TMDbSearchRow, type TMDbSubject } from '../api/tmdbCenter'

const names: Record<string, string> = { movie: '电影', tv: '电视剧', series: '电视剧', season: '季', episode: '集', person: '人物' }
const qualities = ['身份记录', '摘要资料', '基础资料', '完整详情']
const errorText = (e: unknown) => (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '读取失败，请重试'
const validID = (id: string) => /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(id)
import { TMDbDetailModal, type Selection } from './TMDbDetailModal'
import { tmdbDepartment } from './tmdbLabels'
import { CatalogFilters } from '../components/CatalogFilters'
import { CatalogLoadMore } from '../components/CatalogLoadMore'
import { useCatalogContinuation } from '../hooks/useCatalogContinuation'

export function TMDbDataCenterPage() {
  const isAdmin = useAuthStore(s => s.user?.role === 'admin')
  const [params, setParams] = useSearchParams()
  const kind = ['movie', 'tv', 'person'].includes(params.get('kind') || '') ? params.get('kind')! : 'movie'
  const rawCategory = (params.get(kind === 'person' ? 'department' : 'genre') || '').trim()
  const category = kind === 'person' ? Array.from(rawCategory).slice(0, 100).join('') : /^[1-9]\d*$/.test(rawCategory) && Number(rawCategory) <= 2147483647 ? rawCategory : ''
  const [categories, setCategories] = useState<{ value: string; label: string }[]>([])
  const [categoryError, setCategoryError] = useState(false)
  const [categoryRevision, setCategoryRevision] = useState(0)
  useEffect(() => { const c = new AbortController(); setCategories([]); setCategoryError(false); api.categories(kind, c.signal).then(v => { if (!c.signal.aborted) setCategories(v) }).catch(() => { if (!c.signal.aborted) setCategoryError(true) }); return () => c.abort() }, [kind, categoryRevision])
  const keyword = (params.get('keyword') || '').trim().slice(0, 200)
  const quality = isAdmin && ['0', '1', '2', '3'].includes(params.get('quality') || '') ? params.get('quality')! : ''
  const state = isAdmin && ['pending', 'error'].includes(params.get('state') || '') ? params.get('state')! : ''
  const [owned, setOwned] = useState<number[]>([])
  const ownership = useRef<AbortController | null>(null)
  useEffect(() => () => ownership.current?.abort(), [])
  const [draft, setDraft] = useState(keyword)
  const [revision, setRevision] = useState(0)
  const [add, setAdd] = useState(false)
  const trigger = useRef<HTMLElement | null>(null)
  const subjectID = params.get('subject') || '', personID = params.get('person') || ''
  const fetchPage = useCallback(async (page: number, signal: AbortSignal) => {
    if (page === 1) {
      ownership.current?.abort()
      ownership.current = new AbortController()
      setOwned([])
    }
    const ownershipSignal = ownership.current!.signal
    const query = { keyword, kind, page, page_size: 30, state, [kind === 'person' ? 'department' : 'genre']: category, ...(quality ? { quality } : {}) }
    const result = await (kind === 'person' ? api.people(query, signal) : api.subjects(query, signal))
    if (kind !== 'person' && result.data.length && !signal.aborted) {
      // 入库标记独立加载；重置列表时取消所有旧分页的标记请求。
      void discoverAPI.libraryStatus(result.data.map(v => ({ tmdb_id: v.tmdb_id, media_type: kind as 'movie' | 'tv' })), ownershipSignal).then(status => {
        if (!ownershipSignal.aborted && !signal.aborted) setOwned(previous => Array.from(new Set([...previous, ...status.map(v => v.tmdb_id)])))
      }).catch(() => { /* 入库标记失败不阻塞来源资料。 */ })
    }
    return { items: result.data as (TMDbSubject | TMDbPerson)[], total: result.total, page_size: result.page_size }
  }, [keyword, kind, state, category, quality])
  const { items: rows, total, loading, error, hasMore, loadMore, sentinel } = useCatalogContinuation(fetchPage, revision, !!subjectID || !!personID || add)
  const selectedRow = rows.find(v => 'person_id' in v ? v.person_id === personID : v.id === subjectID)
  const selected: Selection | null = validID(personID) ? { id: personID, person: true, title: selectedRow && 'name' in selectedRow ? selectedRow.name : '', image: selectedRow && 'profile_url' in selectedRow ? selectedRow.profile_url : undefined, overview: selectedRow && 'biography' in selectedRow ? selectedRow.biography : undefined } : validID(subjectID) ? { id: subjectID, person: false, title: selectedRow && 'title' in selectedRow ? selectedRow.title : '', image: selectedRow && 'poster_url' in selectedRow ? selectedRow.poster_url : undefined, overview: selectedRow && 'overview' in selectedRow ? selectedRow.overview : undefined } : null
  const change = (values: Record<string, string>) => { const next = new URLSearchParams(params); for (const [k, v] of Object.entries(values)) { if (v) next.set(k, v); else next.delete(k) }; setParams(next) }
  useEffect(() => {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries({ kind, keyword, quality, state, page: '' })) { if (v) next.set(k, v); else next.delete(k) }
    next.delete('type'); next.delete('provider')
    next.delete(kind === 'person' ? 'genre' : 'department')
    if (category) next.set(kind === 'person' ? 'department' : 'genre', category); else next.delete(kind === 'person' ? 'department' : 'genre')
    next.delete('subject'); next.delete('person')
    if (validID(personID)) next.set('person', personID); else if (validID(subjectID)) next.set('subject', subjectID)
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, kind, keyword, quality, state, personID, subjectID, setParams, category])
  useEffect(() => { setDraft(keyword) }, [keyword])
  const open = (v: Selection) => { trigger.current = document.activeElement as HTMLElement; change({ subject: v.person ? '' : v.id, person: v.person ? v.id : '' }) }
  const close = () => { change({ subject: '', person: '' }); requestAnimationFrame(() => trigger.current?.focus()) }
  return <div className="space-y-5">
    <CatalogFilters label="TMDb 类型" options={['movie', 'tv', 'person'].map(value => ({value, label: names[value]}))} value={kind} onChange={value => change({kind: value, genre: '', department: '', page: '', subject: '', person: '', quality: ''})} />
    <CatalogFilters label={kind === 'person' ? '主要职业' : '题材'} options={[{value: '', label: '全部'}, ...categories.map(v => ({...v, label: kind === 'person' ? tmdbDepartment(v.label) : v.label}))]} value={category} onChange={value => change({[kind === 'person' ? 'department' : 'genre']: value, page: ''})} />
    {categoryError && <p role="alert" className="text-sm text-ink-50">分类选项读取失败，可继续搜索资料。<button className="btn-ghost" onClick={() => setCategoryRevision(v => v + 1)}>重试分类</button></p>}
    <CatalogSearchForm label="筛选已保存资料" placeholder="筛选标题、姓名或 TMDB ID…" value={draft} onChange={setDraft} loading={loading} refreshLabel="重新读取列表" onSearch={() => change({keyword:draft.trim(), page:''})} onRefresh={() => setRevision(v => v + 1)}>{isAdmin && <Select aria-label="资料级别" className="input-field w-36" value={quality} onChange={v => change({ quality: v, page: '' })}><option value="">全部资料级别</option>{qualities.map((v, i) => <option key={i} value={String(i)}>{v}</option>)}</Select>}{isAdmin && <Select aria-label="刷新状态" className="input-field w-32" value={state} onChange={v => change({state:v,page:''})}><option value="">全部状态</option><option value="pending">待补全</option><option value="error">刷新失败</option></Select>}{isAdmin && <button type="button" className="btn-outline" onClick={() => setAdd(true)}>搜索 / 添加资料</button>}</CatalogSearchForm>
    {error && <p role="alert">{error} <button className="btn-outline" onClick={loadMore}>重试</button></p>}
    {loading && !rows.length && <p role="status" className="text-ink-50">正在读取资料…</p>}
    {!!rows.length && <p className="text-sm text-ink-50">已显示 {rows.length} / {total}</p>}
    {<><div aria-label="TMDb 资料列表" className="grid grid-cols-2 gap-4 min-[480px]:grid-cols-3 sm:grid-cols-4 md:grid-cols-5">{rows.map(v => { const person = 'person_id' in v; const title = (person ? v.name : v.title) || `TMDb ${v.tmdb_id}`; return <CatalogPosterCard key={v.id} title={title} image={person ? v.profile_url : v.poster_url} person={person} year={person ? tmdbDepartment(v.department) : v.release_date?.slice(0, 4)} rating={person ? undefined : v.rating} owned={!person && owned.includes(v.tmdb_id)} onClick={() => open({ id: person ? v.person_id : v.id, person, title })} /> })}</div>{!loading && !error && !rows.length && <p className="py-8 text-center text-ink-50">暂无符合条件的已保存资料</p>}<div ref={sentinel} aria-hidden="true" /><CatalogLoadMore loading={loading} hasMore={hasMore} hasItems={rows.length > 0} failed={!!error} onLoadMore={loadMore} endLabel="资料已加载完" /></>}

    {selected && <TMDbDetailModal key={`${selected.person}-${selected.id}`} initial={selected} onClose={close} onChanged={() => setRevision(v => v + 1)} />}
    {add && <AddTMDb kind={kind} onClose={() => setAdd(false)} onAdded={v => { setAdd(false); setRevision(n => n + 1); open(v) }} />}
  </div>
}

function AddTMDb({ kind, onClose, onAdded }: { kind: string; onClose: () => void; onAdded: (v: Selection) => void }) {
  const [query, setQuery] = useState(''); const [id, setID] = useState(''); const [rows, setRows] = useState<TMDbSearchRow[]>([]); const [page, setPage] = useState(1); const [pages, setPages] = useState(0); const [pending, setPending] = useState(false); const [error, setError] = useState('')
  const ref = useRef<AbortController | null>(null); useEffect(() => () => ref.current?.abort(), [])
  const search = async (p: number) => { ref.current?.abort(); const c = new AbortController(); ref.current = c; setPending(true); setError(''); try { const v = await api.search(kind, query.trim(), p, c.signal); if (!c.signal.aborted) { setRows(v.results); setPages(v.total_pages); setPage(p) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  const add = async (tmdbID: number) => { if (pending) return; const c = new AbortController(); ref.current = c; setPending(true); setError(''); try { const v = await api.import(kind, tmdbID, c.signal); if (!c.signal.aborted) { toast.success('已登记，正在补充来源资料'); onAdded({ id: v.id, person: kind === 'person', title: '' }) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  return <ModalShell onClose={onClose} ariaLabel="添加 TMDb 资料" maxWidth="max-w-xl" className="max-h-[85vh] overflow-y-auto p-6"><h2 className="text-lg font-semibold">添加{names[kind]}资料</h2><form className="mt-4 flex gap-2" onSubmit={e => { e.preventDefault(); void add(Number(id)) }}><input className="input-field min-w-0 flex-1" aria-label="TMDb ID" placeholder="输入 TMDb ID" inputMode="numeric" value={id} onChange={e => setID(e.target.value)} /><button className="btn-primary" disabled={pending || !/^[1-9]\d*$/.test(id)}>按 ID 添加</button></form><form className="mt-4 flex gap-2" onSubmit={e => { e.preventDefault(); void search(1) }}><input className="input-field min-w-0 flex-1" aria-label="搜索 TMDb" placeholder="输入标题或姓名" value={query} maxLength={100} onChange={e => setQuery(e.target.value)} /><button className="btn-outline" disabled={pending || !query.trim()}>搜索 TMDb</button></form>{error && <p role="alert" className="mt-3 text-red-500">{error}</p>}<div className="mt-4 divide-y divide-[var(--app-border)]">{rows.map(v => <button disabled={pending} key={v.id} className="block w-full py-3 text-left" onClick={() => void add(v.id)}>{v.title || v.name}<span className="ml-2 text-xs text-ink-50">{v.release_date || v.first_air_date} · {v.id}</span></button>)}</div>{pages > 1 && <Pager page={page} size={20} total={Math.min(pages, 500) * 20} setPage={p => void search(p)} />}</ModalShell>
}
