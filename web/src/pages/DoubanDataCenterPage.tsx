import { useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, Database, Film, RefreshCw, Search, ExternalLink, Star } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { doubanAPI, type DoubanRefreshMode, type DoubanSubjectDetail, type DoubanSubjectPage, type DoubanSubjectSummary } from '../api/douban'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import { useAuthStore } from '../stores/auth'
import { MediaCredits } from './MediaDetailCast'

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
  const type = Object.keys(kinds).includes(params.get('type') || '') ? params.get('type')! : ''
  const rawPage = params.get('page') || ''
  const page = /^\d+$/.test(rawPage) && Number(rawPage) >= 1 && Number(rawPage) <= 1_000_000 ? Number(rawPage) : 1
  const [draft, setDraft] = useState(keyword)
  const [data, setData] = useState<DoubanSubjectPage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const [selected, setSelected] = useState<DoubanSubjectSummary | null>(null)
  const [pendingID, setPendingID] = useState<string | null>(null)
  const [actionError, setActionError] = useState<{ id: string; doubanID: string; message: string } | null>(null)
  const mutation = useRef<AbortController | null>(null)
  const detailTrigger = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const next = new URLSearchParams(params)
    for (const [key, value] of [['keyword', keyword], ['type', type], ['page', page > 1 ? String(page) : '']]) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [keyword, page, params, setParams, type])
  useEffect(() => { setDraft(keyword) }, [keyword])
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    setData(null)
    doubanAPI.list(keyword, type, page, controller.signal).then((result) => {
      if (!controller.signal.aborted) setData(result)
    }).catch((err) => {
      if (!controller.signal.aborted) setError(errorText(err, '豆瓣资料列表读取失败'))
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [keyword, type, page, revision])
  useEffect(() => () => mutation.current?.abort(), [])

  const updateParams = (values: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(values)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    setParams(next)
    setSelected(null)
  }
  const openDetail = (subject: DoubanSubjectSummary) => {
    detailTrigger.current = document.activeElement as HTMLElement
    setSelected(subject)
  }
  const closeDetail = () => {
    setSelected(null)
    detailTrigger.current?.focus()
  }
  const refresh = async (subject: DoubanSubjectSummary, mode: DoubanRefreshMode) => {
    if (mutation.current) return
    const controller = new AbortController()
    mutation.current = controller
    setPendingID(subject.id)
    setActionError(null)
    try {
      const result = await doubanAPI.refresh(subject.id, mode, controller.signal)
      if (controller.signal.aborted) return
      if (result.degraded) {
        toast(result.subject.quality === 'complete' ? '完整接口受限，已保留已有完整资料' : '完整接口受限，已保存降级资料')
      } else if (result.supplement_unavailable) toast('基础资料已更新，网页补充暂不可用，已有内容已保留')
      else toast.success(mode === 'base' ? '基础资料已更新，补充资料保持原样' : '基础与补充资料已更新')
    } catch (err) {
      if (!controller.signal.aborted) setActionError({ id: subject.id, doubanID: subject.douban_id, message: errorText(err, '豆瓣资料更新失败，已有数据已保留') })
    } finally {
      if (!controller.signal.aborted) {
        mutation.current = null
        setPendingID(null)
        setRevision((value) => value + 1)
      }
    }
  }
  const actions = (subject: DoubanSubjectSummary) => <div className="flex flex-wrap gap-2">
    <button type="button" className="btn-outline px-3 py-2 text-xs" onClick={() => openDetail(subject)}>查看</button>
    <RefreshButtons pending={pendingID !== null} onRefresh={(mode) => void refresh(subject, mode)} subjectID={subject.douban_id} />
  </div>
  const rows = data?.items || []
  const pages = Math.max(1, Math.ceil((data?.total || 0) / 30))

  return <div className="space-y-5">
    <header className="flex items-start gap-3"><Database className="mt-1 shrink-0 text-brand-500" size={24} /><div><h1 className="font-display text-xl font-semibold text-ink-600">豆瓣资料</h1><p className="mt-1 text-sm text-ink-50">查看已保存的豆瓣信息，按需更新资料与评分。</p></div></header>
    <form className="glass-panel flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); updateParams({ keyword: draft.trim(), page: '' }) }}>
      <label className="min-w-0 flex-1 basis-56 text-xs text-ink-50">名称或豆瓣 ID<input className="input-field mt-1 w-full" aria-label="名称或豆瓣 ID" maxLength={200} value={draft} placeholder="搜索已有资料" onChange={(event) => setDraft(event.target.value)} /></label>
      <label className="w-32 text-xs text-ink-50">类型<Select className="input-field mt-1 w-full" aria-label="资料类型" value={type} onChange={(value) => updateParams({ type: value, page: '' })}><option value="">全部类型</option>{Object.entries(kinds).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</Select></label>
      <button type="submit" className="btn-primary gap-1.5"><Search size={16} />搜索</button>
      <button type="button" className="icon-btn" title="重新读取列表" aria-label="重新读取列表" disabled={loading} onClick={() => setRevision((value) => value + 1)}><RefreshCw size={18} /></button>
    </form>
    {actionError && <p role="alert" className="rounded border border-red-300/40 p-3 text-sm text-red-500">豆瓣 {actionError.doubanID}：{actionError.message}</p>}
    <section className="glass-panel !p-0" aria-label="豆瓣资料列表">
      {loading ? <p className="p-10 text-center text-ink-50">正在读取资料…</p> : error ? <div className="space-y-3 p-8 text-center"><p role="alert" className="text-red-500">{error}</p><button type="button" className="btn-outline" onClick={() => setRevision((value) => value + 1)}>重试读取</button></div> : rows.length === 0 ? <p className="p-10 text-center text-ink-50">{keyword || type ? '没有符合条件的豆瓣资料' : '暂无豆瓣资料，获取或绑定作品后会自动保存到这里'}</p> : <>
        <div className="hidden overflow-x-auto xl:block"><table className="data-table"><thead><tr><th>作品</th><th>类型</th><th>豆瓣评分</th><th>资料状态</th><th>基础资料更新</th><th>补充资料更新</th><th>下次可重试</th><th>操作</th></tr></thead><tbody>{rows.map((subject) => <tr key={subject.id}>
          <td className="max-w-xs"><div className="flex items-center gap-3"><SubjectPoster subject={subject} /><div className="min-w-0"><p className="break-words font-medium text-ink-600">{subject.title || '未获取标题'}</p><p className="mt-1 text-xs text-ink-50">{subject.douban_id}{subject.year ? ` · ${subject.year}` : ''}</p></div></div></td><td className="whitespace-nowrap">{kinds[subject.entity_kind] || subject.entity_kind}</td><td className="font-semibold text-gold-500">{ratingText(subject.rating)}</td><td className="whitespace-nowrap"><Quality subject={subject} /></td><td className="text-xs">{timeText(subject.fetched_at)}</td><td className="text-xs">{timeText(subject.rich_fetched_at || null)}</td><td className="text-xs">{timeText(subject.next_retry_at)}</td><td>{actions(subject)}</td>
        </tr>)}</tbody></table></div>
        <div className="divide-y divide-[var(--app-border)] xl:hidden">{rows.map((subject) => <article key={subject.id} className="space-y-3 p-4"><div className="flex items-start gap-3"><SubjectPoster subject={subject} /><div className="min-w-0 flex-1"><h2 className="break-words font-medium text-ink-600">{subject.title || '未获取标题'}</h2><p className="mt-1 text-xs text-ink-50">{subject.douban_id} · {kinds[subject.entity_kind] || subject.entity_kind}{subject.year ? ` · ${subject.year}` : ''}</p></div><span className="shrink-0 font-semibold text-gold-500">{ratingText(subject.rating)}</span></div><Quality subject={subject} /><dl className="space-y-1 text-xs text-ink-50"><div>基础资料更新：{timeText(subject.fetched_at)}</div><div>补充资料更新：{timeText(subject.rich_fetched_at || null)}</div><div>下次可重试：{timeText(subject.next_retry_at)}</div></dl>{actions(subject)}</article>)}</div>
      </>}
    </section>
    {!loading && !error && <footer className="flex flex-wrap items-center justify-between gap-3 text-sm text-ink-50"><span>共 {data?.total || 0} 条</span>{((data?.total || 0) > 0 || page > 1) && <div className="flex items-center gap-2"><button type="button" className="icon-btn" title="上一页" aria-label="上一页" disabled={page <= 1} onClick={() => updateParams({ page: page === 2 ? '' : String(page - 1) })}><ChevronLeft size={18} /></button><span>{page} / {pages}</span><button type="button" className="icon-btn" title="下一页" aria-label="下一页" disabled={page >= pages} onClick={() => updateParams({ page: String(page + 1) })}><ChevronRight size={18} /></button></div>}</footer>}
    {selected && <SubjectDetails key={selected.id} subject={selected} revision={revision} pending={pendingID !== null} error={actionError?.id === selected.id ? actionError.message : ''} onClose={closeDetail} onRefresh={(mode) => void refresh(selected, mode)} />}
  </div>
}

function SubjectDetails({ subject, revision, pending, error: actionError, onClose, onRefresh }: { subject: DoubanSubjectSummary; revision: number; pending: boolean; error: string; onClose: () => void; onRefresh: (mode: DoubanRefreshMode) => void }) {
  const [detail, setDetail] = useState<DoubanSubjectDetail | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const [loading, setLoading] = useState(true)
  const panel = useRef<HTMLDivElement>(null)
  useEffect(() => { panel.current?.focus() }, [])
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    doubanAPI.detail(subject.id, controller.signal).then((value) => {
      if (!controller.signal.aborted) setDetail(value)
    }).catch((err) => { if (!controller.signal.aborted) setError(errorText(err, '详情读取失败')) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
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
  const trapFocus = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Tab') return
    const focusable = panel.current?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), summary')
    if (!focusable?.length) { event.preventDefault(); return }
    const first = focusable[0], last = focusable[focusable.length - 1]
    if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) { event.preventDefault(); last.focus() }
    else if (!event.shiftKey && (document.activeElement === last || document.activeElement === panel.current)) { event.preventDefault(); first.focus() }
  }
  return <ModalShell onClose={onClose} ariaLabel={item.title || '豆瓣资料'} maxWidth="max-w-5xl" className="max-h-[90dvh] overflow-y-auto">
    <div ref={panel} tabIndex={-1} onKeyDown={trapFocus} className="space-y-6 p-5 outline-none sm:p-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 flex-1 basis-60"><h2 className="break-words font-display text-2xl font-semibold text-ink-600">{item.title || '未获取标题'}{item.year && <span className="ml-2 font-normal text-ink-50">({item.year})</span>}</h2><p className="mt-2 break-words text-xs text-ink-50">{detail?.original_name && `${detail.original_name} · `}{kinds[item.entity_kind]} · 豆瓣 {item.douban_id}</p></div>
        <div className="flex flex-wrap gap-2"><a className="btn-outline gap-1.5 px-3 py-2 text-xs" href={`https://movie.douban.com/subject/${encodeURIComponent(item.douban_id)}/`} target="_blank" rel="noopener noreferrer">豆瓣原页<ExternalLink size={13} /></a><RefreshButtons pending={pending} onRefresh={onRefresh} /></div>
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
        {!!detail.credits?.length && <MediaCredits key={`${item.id}:${detail.rich_fetched_at || ''}`} credits={detail.credits} />}
        {!!detail.awards?.length && <section><h3 className="mb-3 font-display text-base font-semibold text-ink-600">获奖与提名</h3><div className="divide-y divide-[var(--app-border)]">{detail.awards.map((award, index) => <div key={index} className="grid gap-1 py-3 text-xs sm:grid-cols-[14rem_minmax(0,1fr)] sm:gap-4"><p className="text-brand-500">{award.event}</p><p className="break-words text-ink-100">{award.category}{award.people && <span className="ml-2 text-ink-50">{award.people}</span>}</p></div>)}</div></section>}
        {!!detail.honors?.length && <section><h3 className="mb-3 font-display text-base font-semibold text-ink-600">榜单与荣誉</h3><div className="flex flex-wrap gap-2">{detail.honors.map((honor) => <span key={honor.title} className="rounded-lg border border-[var(--app-border)] bg-[var(--app-glass)] px-3 py-2 text-xs text-ink-100">{honor.rank > 0 && <strong className="mr-2 text-gold-500">第 {honor.rank} 名</strong>}{honor.title}</span>)}</div></section>}
      </>}
      <section className="border-t border-[var(--app-border)] pt-5"><div className="mb-3 flex items-center gap-3"><h3 className="text-sm font-medium text-ink-600">资料状态</h3><Quality subject={item} /></div><dl className="grid gap-3 text-xs sm:grid-cols-2">{[['基础资料更新', timeText(item.fetched_at)], ['补充资料更新', timeText(item.rich_fetched_at || null)], ['最近尝试', timeText(item.last_attempt_at)], ['下次可重试', timeText(item.next_retry_at)]].map(([label, value]) => <div key={label}><dt className="text-ink-50">{label}</dt><dd className="mt-1 text-ink-100">{value}</dd></div>)}</dl></section>
      {detail && (detail.has_payload ? <details className="border-t border-[var(--app-border)] pt-4"><summary className="cursor-pointer text-xs text-ink-50">原始豆瓣资料</summary><pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs text-ink-100">{JSON.stringify(detail.raw_json, null, 2)}</pre></details> : <p className="text-sm text-ink-50">该记录尚未成功获取资料。</p>)}
    </div>
  </ModalShell>
}

function RefreshButtons({ pending, onRefresh, subjectID }: { pending: boolean; onRefresh: (mode: DoubanRefreshMode) => void; subjectID?: string }) {
  return <>{([['base', '基础更新'], ['full', '完整更新']] as const).map(([mode, label]) => <button key={mode} type="button" className="btn-outline gap-1.5 px-3 py-2 text-xs" aria-label={`${label}豆瓣${subjectID ? ` ${subjectID}` : '资料'}`} title={mode === 'base' ? '只请求基础接口，更新资料和评分' : '更新基础资料，并尝试获取网页补充'} disabled={pending} onClick={() => onRefresh(mode)}><RefreshCw size={14} />{label}</button>)}</>
}
