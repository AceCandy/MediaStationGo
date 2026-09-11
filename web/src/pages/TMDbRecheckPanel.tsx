import { useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, RefreshCw, Search, Trash2, X } from 'lucide-react'
import { tasksAPI, type TMDbRecheckPage, type TMDbRecheckFilesPage } from '../api/tasks'
import { mediaAPI, type STRMDeleteTarget } from '../api/library'
import { Select } from '../components/Select'
import { ModalShell } from '../components/ModalShell'
import { STRMDeleteDialog } from '../components/STRMDeleteDialog'

const labels: Record<string, string> = { pending: '待检查', running: '处理中', not_found: '上游未收录 / 待核对', retry: '等待重试', blocked: '标识阻塞', done: '已结束' }
type DeleteTarget = { id: string; value: STRMDeleteTarget }

export function TMDbRecheckPanel({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<'all' | 'not_found'>('all')
  const [status, setStatus] = useState('pending')
  const [query, setQuery] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [version, setVersion] = useState(0)
  const [data, setData] = useState<TMDbRecheckPage | null>(null)
  const [error, setError] = useState(false)
  const [target, setTarget] = useState<DeleteTarget | null>(null)
  const [deleted, setDeleted] = useState(false)
  const previewPending = useRef(false)
  useEffect(() => {
    const controller = new AbortController()
    tasksAPI.rechecks(tab === 'not_found' ? 'not_found' : status, page, controller.signal, keyword).then((value) => {
      if (!controller.signal.aborted) setData(value)
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [tab, status, keyword, page, version])
  const reset = () => { setData(null); setError(false) }
  const loading = !data && !error
  return <>
    <ModalShell ariaLabel="季/集复查待办" maxWidth="max-w-4xl" className="flex max-h-[86vh] flex-col" onClose={target ? undefined : onClose}>
    <div className="modal-header shrink-0">
      <h2 className="font-display text-lg font-semibold text-ink-600">季/集复查待办</h2>
      <button type="button" className="icon-btn" aria-label="关闭复查待办" disabled={!!target} onClick={onClose}><X size={18} /></button>
    </div>
    <div className="shrink-0 space-y-4 p-5 pb-0">
      <p className="text-xs text-ink-50">到期后在下一次任务运行时检查；清单未收录或 404 每 3 天复核，不代表文件一定错误。人工清理保留 STRM 和媒体记录。</p>
      {deleted && <p role="status" className="text-xs text-ink-50">本地目标已删除，STRM 和媒体记录保留；待办仍按计划复核。</p>}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap gap-2" role="group" aria-label="待办分类">
          {([['all', '复查待办'], ['not_found', labels.not_found]] as const).map(([value, label]) => <button key={value} type="button" aria-pressed={tab === value} className={`rounded border px-3 py-2 text-sm ${tab === value ? 'border-brand-500 bg-brand-500/10 text-brand-500' : 'border-gray-200 text-ink-50 hover:text-brand-500'}`} onClick={() => { if (tab === value) return; reset(); setTab(value); setPage(1) }}>{label}</button>)}
        </div>
        <button type="button" className="icon-btn" title="刷新复查待办" aria-label="刷新复查待办" disabled={loading} onClick={() => { reset(); setVersion(version + 1) }}><RefreshCw size={16} className={loading ? 'animate-spin' : ''} /></button>
      </div>
      <form className="flex gap-2" onSubmit={(event) => { event.preventDefault(); const value = query.trim(); if (value === keyword) return; reset(); setKeyword(value); setPage(1) }}>
        <label className="relative min-w-0 flex-1">
          <span className="sr-only">搜索季/集复查待办</span>
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-50" />
          <input type="search" className="input-field pl-10" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索剧名、标题、S/E 或 ID" />
        </label>
        <button type="submit" className="btn-outline px-3 py-2 text-xs">搜索</button>
      </form>
      {tab === 'all' && <label className="block sm:w-1/2">
        <span className="mb-1 block text-xs text-ink-50">状态</span>
        <Select className="input-field" aria-label="待办状态" value={status} onChange={(value) => { if (value === status) return; reset(); setStatus(value); setPage(1) }}>
          <option value="">全部状态</option>
          {Object.entries(labels).filter(([value]) => value !== 'not_found').map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </Select>
      </label>}
      {data && <p className="text-xs text-ink-50">{Object.entries(labels).map(([value, label]) => `${label} ${data.counts[value] ?? 0}`).join(' · ')} · 待归并变更 {data.changes}</p>}
    </div>
    <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
      {error ? <p role="alert" className="py-6 text-center text-sm text-red-500">待办加载失败，请重试刷新。</p> : !data ? <p role="status" className="py-6 text-center text-sm text-ink-50">加载中…</p> : <>
        {data.items.length === 0 ? <p className="py-6 text-center text-sm text-ink-50">当前筛选暂无待办。</p> : <div className="space-y-2">
          {data.items.map((item) => <article key={item.metadata_id} className="min-w-0 space-y-3 rounded border border-gray-200 p-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="break-words text-sm font-medium text-ink-600">{item.series_title || item.title || '元数据已删除'} · S{String(item.season_num).padStart(2, '0')}{item.kind === 'episode' ? `E${String(item.episode_num).padStart(2, '0')}` : ''} {item.title}</h3>
                <span className={`text-xs ${item.status === 'not_found' ? 'text-orange-600' : 'text-ink-50'}`}>{labels[item.status] ?? item.status}</span>
              </div>
              <p className="mt-1 break-words text-xs text-ink-50">重试 {item.attempts} 次 · 下次可检查：{item.due_at ? new Date(item.due_at).toLocaleString() : '无'}</p>
              {item.last_error && <p className="mt-1 break-words text-xs text-ink-50">{item.last_error}</p>}
            </div>
            {item.status === 'not_found' && <TMDbRecheckFiles key={`${item.metadata_id}:${version}`} item={item} busy={!!target} pending={previewPending} onTarget={setTarget} />}
          </article>)}
        </div>}
        <div className="flex items-center justify-between text-xs text-ink-50">
          <span>共 {data.total} 条</span>
          <div className="flex items-center gap-2">
            <button type="button" className="icon-btn" aria-label="上一页" disabled={page <= 1} onClick={() => { reset(); setPage(page - 1) }}><ChevronLeft size={16} /></button>
            <span>{page} / {Math.max(1, Math.ceil(data.total / data.page_size))}</span>
            <button type="button" className="icon-btn" aria-label="下一页" disabled={page * data.page_size >= data.total} onClick={() => { reset(); setPage(page + 1) }}><ChevronRight size={16} /></button>
          </div>
        </div>
      </>}
    </div>
    </ModalShell>
    {target && <STRMDeleteDialog mediaID={target.id} target={target.value} onClose={() => setTarget(null)} onDeleted={() => { setTarget(null); setDeleted(true) }} />}
  </>
}

function TMDbRecheckFiles({ item, busy, pending, onTarget }: { item: TMDbRecheckPage['items'][number]; busy: boolean; pending: { current: boolean }; onTarget: (target: DeleteTarget) => void }) {
  const [page, setPage] = useState(1)
  const [data, setData] = useState<TMDbRecheckFilesPage | null>(null)
  const [error, setError] = useState('')
  const [version, setVersion] = useState(0)
  const [previewing, setPreviewing] = useState(false)
  const alive = useRef(true)
  useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
  useEffect(() => {
    const controller = new AbortController()
    tasksAPI.recheckFiles(item.metadata_id, page, controller.signal).then((value) => {
      if (!controller.signal.aborted) setData(value)
    }).catch(() => { if (!controller.signal.aborted) setError('关联文件加载失败，请刷新重试。') })
    return () => controller.abort()
  }, [item.metadata_id, page, version])
  const preview = async (id: string) => {
    if (pending.current || busy) return
    pending.current = true
    setPreviewing(true)
    setError('')
    try {
      const value = await mediaAPI.getSTRMDeleteTarget(id)
      if (alive.current) onTarget({ id, value })
    } catch {
      if (alive.current) setError('无法安全解析本地目标：请检查文件是否存在、STRM 本地映射及可信根配置。未删除任何文件。')
    } finally {
      pending.current = false
      if (alive.current) setPreviewing(false)
    }
  }
  const reset = () => { setData(null); setError('') }
  return <div className="space-y-2 text-xs">
        {error && <p role="alert" className="text-red-500">{error}</p>}
        {!data ? !error && <p role="status">加载中…</p> : data.items.length === 0 ? <p>暂无关联文件，或待办已不再属于未收录分类。</p> : <ul className="divide-y divide-gray-200">
          {data.items.map((file) => <li key={file.media_id} className="flex min-w-0 flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <p className="break-all text-ink-100">{file.path}</p>
              {!file.can_preview && <p className="mt-1 text-ink-50">不支持 STRM 本地目标清理</p>}
            </div>
            <button type="button" className="btn-danger shrink-0 px-3 py-2 text-xs" disabled={!file.can_preview || previewing || busy} onClick={() => void preview(file.media_id)}><Trash2 size={14} aria-hidden="true" /> 删除</button>
          </li>)}
        </ul>}
        {error && <button type="button" className="btn-outline" disabled={previewing || busy} onClick={() => { reset(); setVersion(version + 1) }}>重试加载文件</button>}
        {(page > 1 || data?.has_more) && <div className="flex flex-wrap items-center gap-3">
          <button type="button" className="btn-outline" disabled={page <= 1 || previewing || busy} onClick={() => { reset(); setPage(page - 1) }}>上一页</button>
          <span>文件第 {page} 页</span>
          <button type="button" className="btn-outline" disabled={!data?.has_more || previewing || busy} onClick={() => { reset(); setPage(page + 1) }}>下一页</button>
        </div>}
  </div>
}
