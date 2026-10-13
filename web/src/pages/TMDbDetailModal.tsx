import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, Film, UserRound } from 'lucide-react'
import { ModalShell } from '../components/ModalShell'
import { CatalogPager as Pager } from '../components/CatalogPager'
import { Select } from '../components/Select'
import { CatalogPosterCard } from '../components/CatalogPosterCard'
import { useAuthStore } from '../stores/auth'
import { imageURL } from '../api/client'
import { tmdbCenterAPI as api, type TMDbCredit, type TMDbDetail, type TMDbPage, type TMDbSubject } from '../api/tmdbCenter'
import { tmdbDepartment, tmdbStatus } from './tmdbLabels'
import { MediaCredits } from './MediaDetailCast'
const names: Record<string, string> = { movie: '电影', tv: '电视剧', series: '电视剧', season: '季', episode: '集', person: '人物' }
const date = (v?: string) => v && !v.startsWith('0001-') ? new Date(v).toLocaleString() : '时间未知'
const errorText = (e: unknown) => (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '读取失败，请重试'
export type Selection = { id: string; person: boolean; title: string; image?: string; overview?: string }

function Picture({ src, title, person = false, large = false }: { src?: string; title: string; person?: boolean; large?: boolean }) {
  const url = imageURL(src, undefined, { maxWidth: large ? 600 : 160 })
  const [failed, setFailed] = useState('')
  return <div className={`shrink-0 overflow-hidden bg-[var(--app-glass)] ${person ? 'aspect-square rounded-full' : 'aspect-[2/3] rounded-lg'} ${large ? 'w-[88px] sm:w-40' : 'w-12'}`}>
    {url && failed !== url ? <img src={url} alt={title} className="h-full w-full object-cover" loading="lazy" decoding="async" onError={() => setFailed(url)} /> : <div className="flex h-full items-center justify-center text-ink-50">{person ? <UserRound /> : <Film />}</div>}
  </div>
}
export function TMDbDetailModal({ initial, onClose, onChanged }: { initial: Selection; onClose: () => void; onChanged?: () => void }) {
  const [history, setHistory] = useState([initial]); const selected = history[history.length - 1]
  return <ModalShell onClose={onClose} ariaLabel="TMDb 来源资料" maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto p-5 sm:p-8">
    {history.length > 1 && <button className="btn-outline mb-5" onClick={() => setHistory(v => v.slice(0, -1))}><ArrowLeft size={16} />返回 {history[history.length - 2].title || '上一层'}</button>}
    <TMDbDetailContent key={`${selected.person}-${selected.id}`} selected={selected} onOpen={v => setHistory(h => [...h.map((x, i) => i === h.length - 1 ? { ...x, title: x.title || selected.title } : x), v])} onChanged={onChanged || (() => {})} />
  </ModalShell>
}

function TMDbDetailContent({ selected, onOpen, onChanged }: { selected: Selection; onOpen: (v: Selection) => void; onChanged: () => void }) {
  const isAdmin = useAuthStore(s => s.user?.role === 'admin')
  const [detail, setDetail] = useState<TMDbDetail | null>(null); const [error, setError] = useState(''); const [revision, setRevision] = useState(0)
  const [pollPaused, setPollPaused] = useState(false)
  const [pending, setPending] = useState(false); const [works, setWorks] = useState(!selected.person)
  const [relatedRevision, setRelatedRevision] = useState(0)
  const [childLoading, setChildLoading] = useState(false); const [creditLoading, setCreditLoading] = useState(false)
  const [childError, setChildError] = useState(''); const [creditError, setCreditError] = useState('')
  const [children, setChildren] = useState<TMDbPage<TMDbSubject> | null>(null); const [childPage, setChildPage] = useState(1)
  const [credits, setCredits] = useState<TMDbPage<TMDbCredit> | null>(null); const [creditPage, setCreditPage] = useState(1)
  const [workKind, setWorkKind] = useState(''); const [workCategory, setWorkCategory] = useState(''); const [workOrder, setWorkOrder] = useState('newest')
  const [imageRevision, setImageRevision] = useState(0)
  const mutation = useRef<AbortController | null>(null)
  const ensured = useRef(false)
  useEffect(() => () => mutation.current?.abort(), [])
  useEffect(() => {
    const c = new AbortController(); let timer: ReturnType<typeof setTimeout> | undefined; let reads = 0; setPollPaused(false)
    const load = () => api.detail(selected.id, selected.person, c.signal).then(v => { if (c.signal.aborted) return; setDetail(v); setError(''); if (v.refresh_pending && ++reads < 45) timer = setTimeout(load, 2000); else if (v.refresh_pending) setPollPaused(true) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) })
    void load(); return () => { c.abort(); clearTimeout(timer) }
  }, [selected.id, selected.person, revision])
  useEffect(() => {
    if (!detail || detail.source_error || ensured.current || !detail.missing_scopes.includes('detail')) return
    ensured.current = true
    const c = new AbortController(); mutation.current = c
    api.ensure(selected.id, selected.person, 'detail', c.signal).then(() => { if (!c.signal.aborted) setRevision(v => v + 1) }).catch(e => { if (!c.signal.aborted) setError(errorText(e)) })
  }, [detail, selected.id, selected.person])
  const subject = detail?.subject; const person = detail?.person; const kind = subject?.kind
  const ready = !!detail
  const stamp = detail?.components.map(v => v.fetched_at).join(',')
  useEffect(() => {
    if (!kind || !['series', 'season'].includes(kind)) return
    const c = new AbortController(); setChildLoading(true); setChildError(''); api.children(selected.id, kind, childPage, c.signal).then(v => { if (!c.signal.aborted) setChildren(v) }).catch(e => { if (!c.signal.aborted) setChildError(errorText(e)) }).finally(() => { if (!c.signal.aborted) setChildLoading(false) }); return () => c.abort()
  }, [kind, selected.id, childPage, stamp, relatedRevision])
  useEffect(() => {
    if (!works || !ready) return
    const c = new AbortController(); setCreditLoading(true); setCreditError(''); api.credits(selected.id, selected.person, creditPage, c.signal, {kind: workKind, category: workCategory, order: workOrder}).then(v => { if (!c.signal.aborted) setCredits(v) }).catch(e => { if (!c.signal.aborted) setCreditError(errorText(e)) }).finally(() => { if (!c.signal.aborted) setCreditLoading(false) }); return () => c.abort()
  }, [selected.id, selected.person, works, creditPage, stamp, workKind, workCategory, workOrder, relatedRevision, ready])
  const refresh = async (scope: string) => { if (pending) return; const c = new AbortController(); mutation.current = c; setPending(true); setError(''); try { await api.refresh(selected.id, selected.person, scope, c.signal); if (!c.signal.aborted) { setRevision(v => v + 1); onChanged() } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  const retryImage = async () => { const c = new AbortController(); mutation.current = c; setPending(true); try { await api.retryImage(selected.person ? 'person' : kind!, (detail?.person || detail?.subject)!.tmdb_id, selected.person ? 'profile' : kind === 'episode' ? 'still' : 'poster', c.signal); if (!c.signal.aborted) { setImageRevision(v => v + 1); setRevision(v => v + 1) } } catch (e) { if (!c.signal.aborted) setError(errorText(e)) } finally { if (!c.signal.aborted) setPending(false) } }
  if (!detail) return <div className="space-y-5"><div className="flex gap-5"><Picture src={selected.image} title={selected.title} person={selected.person} large /><div className="min-w-0 space-y-3"><h2 className="break-words font-display text-2xl font-semibold">{selected.title || 'TMDB 来源资料'}</h2>{selected.overview && <p className="whitespace-pre-line break-words text-sm leading-7 text-ink-50">{selected.overview}</p>}</div></div><p role={error ? 'alert' : 'status'}>{error || '正在读取来源资料…'}{error && <button className="btn-outline" onClick={() => setRevision(v => v + 1)}>重试</button>}</p></div>
  const title = person?.name || subject?.title || '来源资料'; const fields = person?.fields || subject?.fields || {}
  const busy = pending || detail.refresh_pending
  const imdb = fields.external_ids?.imdb_id || fields.imdb_id
  return <div className="space-y-7">
    {!!detail.parents?.length && <nav aria-label="资料层级" className="flex flex-wrap items-center gap-2 text-sm">{detail.parents.map(p => <span key={p.id}><button className="text-accent-400 hover:underline" onClick={() => onOpen({id:p.id,person:false,title:p.title})}>{p.title || (p.kind === 'series' ? '电视剧' : `第 ${p.number} 季`)}</button> / </span>)}<span>{title}</span></nav>}
    <header className="relative isolate overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] p-5 sm:p-7">
      {subject?.backdrop_url && <img src={imageURL(subject.backdrop_url, undefined, {maxWidth:1280})} alt="" className="pointer-events-none absolute inset-0 -z-10 h-full w-full object-cover opacity-[0.08]" onError={e => { e.currentTarget.style.display = 'none' }} />}
      <div className="grid grid-cols-[88px_1fr] items-start gap-4 sm:gap-6 sm:grid-cols-[160px_1fr] lg:grid-cols-[160px_1fr_140px]">
        <Picture src={person?.profile_url || subject?.poster_url ? `${person?.profile_url || subject?.poster_url}${(person?.profile_url || subject?.poster_url)?.includes('?') ? '&' : '?'}retry=${imageRevision}` : undefined} title={title} person={selected.person} large />
        <div className="contents sm:block"><div className="min-w-0"><p className="text-xs font-semibold tracking-widest text-ink-50">TMDB · {person ? '人物' : names[kind || 'movie']}</p><h2 className="mt-2 break-words font-display text-2xl font-semibold sm:text-3xl">{title}</h2>
          {(person?.original_name || subject?.original_name) !== title && <p className="mt-2 text-sm text-ink-50">{person?.original_name || subject?.original_name}</p>}
          </div><dl className="col-span-2 mt-1 grid sm:mt-5 grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm leading-6">{[
            ['类型', fields.genres?.map(v => v.name).join(' / ')], [kind === 'movie' ? '上映日期' : '首播日期', subject?.release_date], ['状态', tmdbStatus(subject?.status)],
            ['时长', fields.runtime ? `${fields.runtime} 分钟` : fields.episode_run_time?.length ? `${fields.episode_run_time.join(' / ')} 分钟 / 集` : ''],
            ['地区', fields.production_countries?.map(v => v.name).join(' / ')], ['语言', fields.spoken_languages?.map(v => v.name).join(' / ')],
            ['主要职业', tmdbDepartment(person?.department)], ['出生日期', fields.birthday], ['逝世日期', fields.deathday], ['出生地', fields.place_of_birth],
          ].filter(([, value]) => value).map(([label, value]) => <div key={label} className="contents"><dt className="text-ink-50">{label}</dt><dd className="min-w-0 break-words">{value}</dd></div>)}</dl>
          <div className="col-span-2 mt-1 flex flex-wrap items-center gap-2 sm:mt-5">{detail.source_url && <a className="btn-outline" target="_blank" rel="noreferrer" href={detail.source_url}>TMDB ↗</a>}{imdb && (person ? /^nm\d+$/ : /^tt\d+$/).test(imdb) && <a className="btn-outline" href={`https://www.imdb.com/${person ? 'name' : 'title'}/${imdb}/`} target="_blank" rel="noreferrer">IMDb ↗</a>}
            {isAdmin && <details className="relative"><summary className="btn-outline cursor-pointer">资料管理</summary><div className="mt-2 flex flex-wrap gap-2 rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)] p-3"><button disabled={busy} className="btn-outline" onClick={() => void refresh('detail')}>{busy ? '正在更新…' : '刷新来源资料'}</button>{subject && kind !== 'episode' && <button disabled={busy} className="btn-outline" onClick={() => void refresh('base')}>仅基础资料</button>}{person && <button disabled={busy} className="btn-outline" onClick={() => { setWorks(true); void refresh('filmography') }}>刷新作品履历</button>}<button disabled={busy} className="btn-outline" onClick={() => void retryImage()}>重试图片</button></div></details>}
          </div>
        </div>
        {subject && <aside aria-label="TMDB 评分" className="col-span-2 flex flex-wrap items-baseline gap-x-3 border-t border-[var(--app-border)] pt-3 sm:col-span-1 sm:col-start-2 sm:block lg:col-start-auto lg:border-l lg:border-t-0 lg:pl-5 lg:pt-0"><p className="text-xs tracking-wide text-ink-50">TMDB 评分</p><p className="mt-2 text-4xl font-semibold tabular-nums text-gold-500">{subject.vote_count > 0 && subject.rating != null ? subject.rating.toFixed(1) : '—'}</p><p className="mt-2 text-xs text-ink-50">{subject.vote_count > 0 ? `${subject.vote_count.toLocaleString()} 人评分` : '暂无评分'}</p></aside>}
      </div>
    </header>
    {pollPaused && <p role="status" className="text-sm text-ink-50">资料仍在后台补全。<button className="btn-outline ml-2" onClick={() => setRevision(v => v + 1)}>重新读取</button></p>}
    {error && <p role="alert" className="text-red-500">{error}<button className="btn-outline ml-2" onClick={() => { ensured.current = false; setRevision(v => v + 1) }}>重试读取</button></p>}
    {detail.missing_scopes.includes('detail') && !busy && <p className="rounded-lg bg-[var(--app-glass)] p-3 text-sm text-ink-50">尚未保存完整资料。{isAdmin ? '可点击“刷新来源资料”补齐这一项。' : '请等待资料更新。'}</p>}
    {subject?.kind === 'series' && <p className="text-sm text-ink-50">{fields.number_of_seasons != null && `${fields.number_of_seasons} 季`}{fields.number_of_episodes != null && ` · ${fields.number_of_episodes} 集`}{fields.last_air_date && ` · 最近播出：${fields.last_air_date}`}</p>}
    <section className="space-y-3"><h3 className="font-display text-lg font-semibold">{person ? '人物简介' : '剧情简介'}</h3><p className="whitespace-pre-line text-sm leading-7 text-ink-400">{person?.biography || subject?.overview || '暂无简介'}</p></section>
    {!!fields.also_known_as?.length && <p className="text-sm text-ink-50">别名：{fields.also_known_as.join(' / ')}</p>}
    {(childLoading || creditLoading) && <p role="status" className="text-sm text-ink-50">正在读取关联资料，已有结果保持显示…</p>}
    {(childError || creditError) && <p role="alert" className="text-sm text-red-500">{childError && `季集读取失败，已保留上次结果：${childError}`} {creditError && `演职关系读取失败，已保留上次结果：${creditError}`}<button className="btn-outline ml-2" onClick={() => setRelatedRevision(v => v + 1)}>重试关联资料</button></p>}
    {children && children.total > 0 && <section aria-label="季集资料" className="space-y-4"><h3 className="font-display text-lg font-semibold">{kind === 'series' ? '季' : '集'} · {children.total}</h3><div className={kind === 'series' ? 'grid grid-cols-2 gap-4 sm:grid-cols-4' : 'grid gap-4'}>{children.data.map(v => kind === 'series' ? <CatalogPosterCard key={v.id} title={v.title || (v.number === 0 ? '特别篇' : `第 ${v.number} 季`)} image={v.poster_url} year={v.release_date} rating={v.rating} onClick={() => onOpen({id:v.id, person:false, title:v.title})} /> : <button key={v.id} className="grid gap-4 rounded-xl border border-[var(--app-border)] p-3 text-left sm:grid-cols-[180px_1fr]" onClick={() => onOpen({id:v.id, person:false, title:v.title})}><div className="aspect-video overflow-hidden rounded-lg bg-[var(--app-glass)]">{v.poster_url ? <img src={imageURL(v.poster_url, undefined, {maxWidth:400})} alt="" loading="lazy" className="h-full w-full object-cover" onError={e => {e.currentTarget.style.display='none'}} /> : <Film className="mx-auto mt-8 text-ink-50" />}</div><div className="min-w-0"><p className="font-semibold">第 {v.number} 集 · {v.title}</p><p className="mt-1 text-xs text-ink-50">{v.release_date}{v.rating ? ` · ★ ${v.rating.toFixed(1)}` : ''}</p><p className="mt-2 line-clamp-3 text-sm leading-6 text-ink-50">{v.overview || '暂无简介'}</p></div></button>)}</div><Pager page={children.page} total={children.total} disabled={childLoading} setPage={page => { setChildPage(page); setRelatedRevision(v => v + 1) }} /></section>}
    {person && !works && <button className="btn-outline" onClick={() => setWorks(true)}>查看作品履历</button>}
    {person && works && <div className="flex flex-wrap gap-2"><Select aria-label="作品类型" className="input-field w-32" value={workKind} onChange={v => {setWorkKind(v);setCreditPage(1)}}><option value="">全部作品</option><option value="movie">电影</option><option value="tv">电视剧</option></Select><Select aria-label="参演或幕后" className="input-field w-32" value={workCategory} onChange={v => {setWorkCategory(v);setCreditPage(1)}}><option value="">全部职务</option><option value="cast">参演</option><option value="crew">幕后</option></Select><Select aria-label="作品年份排序" className="input-field w-32" value={workOrder} onChange={v => {setWorkOrder(v);setCreditPage(1)}}><option value="newest">最新优先</option><option value="oldest">最早优先</option></Select></div>}
    {works && credits && <section aria-label="演职关系资料" className="space-y-4">{person ? <><h3 className="font-display text-lg font-semibold">参演与幕后作品</h3><div className="grid grid-cols-2 gap-4 sm:grid-cols-4">{credits.data.map(v => <CatalogPosterCard key={v.id} title={v.subject.title} image={v.subject.poster_url} rating={v.subject.rating} year={[v.subject.release_date?.slice(0, 4), v.character, v.job].filter(Boolean).join(' · ')} onClick={() => onOpen({id:v.subject.id, person:false, title:v.subject.title})} />)}</div></> : <MediaCredits credits={credits.data.map(v => ({person_id:v.person.person_id, name:v.person.name, profile_url:v.person.profile_url, type:v.category, role:[v.character,v.job].filter(Boolean).join(' / ')}))} onPersonClick={v => onOpen({id:v.person_id, person:true, title:v.name})} />}{!credits.data.length && <p className="text-sm text-ink-50">暂无已保存关系</p>}<Pager page={credits.page} total={credits.total} size={60} disabled={creditLoading} setPage={page => { setCreditPage(page); setRelatedRevision(v => v + 1) }} /></section>}
    {isAdmin && <details className="rounded-lg border border-[var(--app-border)] p-4 text-sm"><summary className="cursor-pointer">来源记录与刷新状态</summary><div className="mt-4 space-y-3">{(detail.jobs || []).map(j => <p key={j.id}>{j.scope} · {j.language} · {j.status} · 上次成功 {date(j.last_success_at)}{j.error && ` · ${j.error}`}</p>)}{detail.components.map(v => <details key={`${v.component}-${v.language}`}><summary className="cursor-pointer">{v.component} · {v.language} · {date(v.fetched_at)}</summary><pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded bg-[var(--app-glass)] p-3 text-xs">{JSON.stringify(v.raw_json, null, 2)}</pre></details>)}</div></details>}
  </div>
}
