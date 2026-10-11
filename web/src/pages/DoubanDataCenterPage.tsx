import { useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, Database, Film, RefreshCw, Search, X } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { doubanAPI, type DoubanSubjectDetail, type DoubanSubjectPage, type DoubanSubjectSummary } from '../api/douban'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import { useAuthStore } from '../stores/auth'

const kinds: Record<string, string> = { movie: '电影', series: '电视剧' }
const qualityLabels: Record<string, string> = { complete: '完整资料', degraded: '降级资料', partial: '摘要资料' }
const canRefresh = (subject: DoubanSubjectSummary) => ['movie', 'series'].includes(subject.entity_kind)
const timeText = (value: string | null) => value ? new Date(value).toLocaleString() : '—'
const ratingText = (value: number) => value > 0 ? value.toFixed(1) : '—'
const errorText = (error: unknown, fallback: string) => (error as { response?: { data?: { message?: string } } })?.response?.data?.message || fallback

function Quality({ subject }: { subject: DoubanSubjectSummary }) {
  return <span className={subject.quality === 'complete' ? 'badge-sage' : 'badge-neutral'}>{subject.has_payload ? qualityLabels[subject.quality] || '已有资料' : '未获取资料'}</span>
}

function SubjectPoster({ subject, large = false }: { subject: DoubanSubjectSummary; large?: boolean }) {
  const src = imageURL(subject.poster_url, subject.fetched_at || undefined, { maxWidth: large ? 600 : 160 })
  const [failedURL, setFailedURL] = useState('')
  return <div className={`flex aspect-[2/3] shrink-0 items-center justify-center overflow-hidden rounded bg-[var(--app-glass)] ${large ? 'w-40 sm:w-48' : 'w-12'}`}>
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
  const refresh = async (subject: DoubanSubjectSummary) => {
    if (mutation.current || !canRefresh(subject)) return
    const controller = new AbortController()
    mutation.current = controller
    setPendingID(subject.id)
    setActionError(null)
    try {
      const result = await doubanAPI.refresh(subject.id, controller.signal)
      if (controller.signal.aborted) return
      if (result.degraded) {
        toast(result.subject.quality === 'complete' ? '完整接口受限，已保留已有完整资料' : '完整接口受限，已保存降级资料')
      } else toast.success('豆瓣资料已更新')
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
    {canRefresh(subject) ? <button type="button" className="btn-outline gap-1.5 px-3 py-2 text-xs" aria-label={`更新豆瓣 ${subject.douban_id}`} disabled={pendingID !== null} onClick={() => void refresh(subject)}><RefreshCw size={14} className={pendingID === subject.id ? 'animate-spin' : ''} />{pendingID === subject.id ? '更新中…' : '更新'}</button> : <span className="self-center text-xs text-ink-50">历史资料 · 仅查看</span>}
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
        <div className="hidden overflow-x-auto xl:block"><table className="data-table"><thead><tr><th>作品</th><th>类型</th><th>豆瓣评分</th><th>资料状态</th><th>最近获取</th><th>下次可重试</th><th>操作</th></tr></thead><tbody>{rows.map((subject) => <tr key={subject.id}>
          <td className="max-w-xs"><div className="flex items-center gap-3"><SubjectPoster subject={subject} /><div className="min-w-0"><p className="break-words font-medium text-ink-600">{subject.title || '未获取标题'}</p><p className="mt-1 text-xs text-ink-50">{subject.douban_id}{subject.year ? ` · ${subject.year}` : ''}</p></div></div></td><td className="whitespace-nowrap">{kinds[subject.entity_kind] || subject.entity_kind}</td><td className="font-semibold text-gold-500">{ratingText(subject.rating)}</td><td className="whitespace-nowrap"><Quality subject={subject} /></td><td className="text-xs">{timeText(subject.fetched_at)}</td><td className="text-xs">{timeText(subject.next_retry_at)}</td><td>{actions(subject)}</td>
        </tr>)}</tbody></table></div>
        <div className="divide-y divide-[var(--app-border)] xl:hidden">{rows.map((subject) => <article key={subject.id} className="space-y-3 p-4"><div className="flex items-start gap-3"><SubjectPoster subject={subject} /><div className="min-w-0 flex-1"><h2 className="break-words font-medium text-ink-600">{subject.title || '未获取标题'}</h2><p className="mt-1 text-xs text-ink-50">{subject.douban_id} · {kinds[subject.entity_kind] || subject.entity_kind}{subject.year ? ` · ${subject.year}` : ''}</p></div><span className="shrink-0 font-semibold text-gold-500">{ratingText(subject.rating)}</span></div><Quality subject={subject} /><dl className="space-y-1 text-xs text-ink-50"><div>最近获取：{timeText(subject.fetched_at)}</div><div>下次可重试：{timeText(subject.next_retry_at)}</div></dl>{actions(subject)}</article>)}</div>
      </>}
    </section>
    {!loading && !error && <footer className="flex flex-wrap items-center justify-between gap-3 text-sm text-ink-50"><span>共 {data?.total || 0} 条</span>{((data?.total || 0) > 0 || page > 1) && <div className="flex items-center gap-2"><button type="button" className="icon-btn" title="上一页" aria-label="上一页" disabled={page <= 1} onClick={() => updateParams({ page: page === 2 ? '' : String(page - 1) })}><ChevronLeft size={18} /></button><span>{page} / {pages}</span><button type="button" className="icon-btn" title="下一页" aria-label="下一页" disabled={page >= pages} onClick={() => updateParams({ page: String(page + 1) })}><ChevronRight size={18} /></button></div>}</footer>}
    {selected && <SubjectDetails key={selected.id} subject={selected} revision={revision} pending={pendingID !== null} error={actionError?.id === selected.id ? actionError.message : ''} onClose={closeDetail} onRefresh={() => void refresh(selected)} />}
  </div>
}

function SubjectDetails({ subject, revision, pending, error: actionError, onClose, onRefresh }: { subject: DoubanSubjectSummary; revision: number; pending: boolean; error: string; onClose: () => void; onRefresh: () => void }) {
  const [detail, setDetail] = useState<DoubanSubjectDetail | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const [loading, setLoading] = useState(true)
  const closeButton = useRef<HTMLButtonElement>(null)
  useEffect(() => { closeButton.current?.focus() }, [])
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
  return <ModalShell onClose={onClose} ariaLabel="豆瓣资料详情" maxWidth="max-w-3xl" className="max-h-[90dvh] overflow-y-auto">
    <header className="modal-header"><div className="min-w-0"><h2 className="break-words text-lg font-semibold text-ink-600">{item.title || '豆瓣资料详情'}</h2><p className="mt-1 text-sm text-ink-50">{item.douban_id} · {kinds[item.entity_kind]}</p></div><button ref={closeButton} type="button" className="icon-btn shrink-0" title="关闭详情" aria-label="关闭详情" onClick={onClose}><X size={20} /></button></header>
    <div className="space-y-5 px-5 py-4">
      <div className="flex flex-col gap-4 sm:flex-row"><SubjectPoster subject={item} large /><div className="min-w-0 flex-1 space-y-4"><div className="flex flex-wrap items-center gap-3"><Quality subject={item} /><span className="font-semibold text-gold-500">豆瓣评分 {ratingText(item.rating)}</span>{item.year && <span className="text-sm text-ink-50">{item.year} 年</span>}</div><dl className="grid gap-3 text-sm sm:grid-cols-2">{[['最近获取', timeText(item.fetched_at)], ['最近尝试', timeText(item.last_attempt_at)], ['下次可重试', timeText(item.next_retry_at)]].map(([label, value]) => <div key={label}><dt className="text-xs text-ink-50">{label}</dt><dd className="mt-1 text-ink-600">{value}</dd></div>)}</dl></div></div>
      {loading && <p className="text-sm text-ink-50">正在读取详情…</p>}
      {error && <div className="flex flex-wrap items-center gap-3"><p role="alert" className="text-sm text-red-500">{error}</p><button type="button" className="btn-outline px-3 py-2 text-xs" onClick={() => setRetry((value) => value + 1)}>重试读取详情</button></div>}
      {actionError && <p role="alert" className="text-sm text-red-500">{actionError}</p>}
      {detail && <>
        <dl className="grid gap-3 text-sm sm:grid-cols-2">{[['原名', detail.original_name], ['上映日期', detail.release_date], ['类型', detail.genres?.join('、')], ['语言', detail.languages?.join('、')], ['地区', detail.countries?.join('、')]].map(([label, value]) => <div key={label}><dt className="text-xs text-ink-50">{label}</dt><dd className="mt-1 break-words text-ink-600">{value || '—'}</dd></div>)}</dl>
        <section><h3 className="mb-2 text-sm font-medium text-ink-600">简介</h3><p className="whitespace-pre-wrap break-words text-sm leading-relaxed text-ink-100">{detail.overview || '暂无简介'}</p></section>
        {detail.has_payload ? <details className="rounded border border-[var(--app-border)] p-3"><summary className="cursor-pointer text-sm text-ink-100">原始豆瓣资料</summary><pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs text-ink-100">{JSON.stringify(detail.raw_json, null, 2)}</pre></details> : <p className="text-sm text-ink-50">该记录尚未成功获取资料。</p>}
      </>}
    </div>
    <footer className="modal-footer mt-5">{canRefresh(item) ? <button type="button" className="btn-primary gap-2" disabled={pending} onClick={onRefresh}><RefreshCw size={16} className={pending ? 'animate-spin' : ''} />{pending ? '正在更新…' : '更新豆瓣资料'}</button> : <p className="text-sm text-ink-50">历史季和集资料仅支持查看</p>}</footer>
  </ModalShell>
}
