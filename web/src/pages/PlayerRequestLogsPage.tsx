import { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, RefreshCw, ScrollText, X } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import { adminAPI, type PlayerRequestLog, type PlayerRequestLogPage } from '../api/admin'
import { ModalShell } from '../components/ModalShell'
import { useSSE } from '../hooks/useSSE'

const PAGE_SIZE = 50
const methods = ['', 'GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']

function currentUTCMonth(): string {
  return new Date().toISOString().slice(0, 7)
}

function positiveInt(value: string | null, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

function statusClass(status: number): string {
  if (status >= 500) return 'badge-neutral text-red-500'
  if (status >= 400) return 'badge-gold'
  if (status >= 300) return 'badge-sage'
  return 'badge-brand'
}

function LogDetails({ log, onClose }: { log: PlayerRequestLog; onClose: () => void }) {
  const sections = [
    ['Body', log.body],
    ['错误响应', log.response_body],
    ['Path 参数', log.path_params],
    ['Header', log.headers],
    ['Query', log.query],
  ] as const
  return (
    <ModalShell onClose={onClose} maxWidth="max-w-4xl" className="max-h-[90vh] overflow-auto p-4 sm:p-6" ariaLabel="播放器请求详情">
      <header className="flex items-start justify-between gap-3">
        <div><h2 className="font-display text-lg font-semibold text-ink-600">请求详情</h2><p className="mt-1 break-all font-mono text-xs text-ink-50">{log.method} {log.route}</p></div>
        <button type="button" className="icon-btn" aria-label="关闭" title="关闭" onClick={onClose}><X size={18} /></button>
      </header>
      <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-3">
        <div><dt className="text-ink-50">时间</dt><dd className="mt-1 text-ink-600">{new Date(log.requested_at).toLocaleString()}</dd></div>
        <div><dt className="text-ink-50">状态 / 耗时</dt><dd className="mt-1 text-ink-600">{log.status} · {log.duration_ms} ms</dd></div>
        <div><dt className="text-ink-50">来源 IP</dt><dd className="mt-1 font-mono text-ink-600">{log.ip || '-'}</dd></div>
      </dl>
      <div className="mt-5 space-y-4">
        {sections.map(([label, value]) => (
          <section key={label}><h3 className="mb-2 text-sm font-semibold text-ink-600">{label}</h3><pre className="max-h-64 overflow-auto rounded-lg bg-gray-950 p-3 text-xs leading-relaxed text-gray-100">{typeof value === 'string' ? value || '-' : JSON.stringify(value ?? {}, null, 2)}</pre></section>
        ))}
      </div>
    </ModalShell>
  )
}

export function PlayerRequestLogsPage() {
  const [params, setParams] = useSearchParams()
  const month = /^\d{4}-(0[1-9]|1[0-2])$/.test(params.get('month') ?? '') ? params.get('month')! : currentUTCMonth()
  const page = positiveInt(params.get('page'), 1)
  const method = methods.includes(params.get('method') ?? '') ? params.get('method') ?? '' : ''
  const statusValue = positiveInt(params.get('status'), 0)
  const status = statusValue >= 100 && statusValue <= 599 ? statusValue : undefined
  const path = params.get('path') ?? ''
  const [pathDraft, setPathDraft] = useState(path)
  const [statusDraft, setStatusDraft] = useState(status ? String(status) : '')
  const [data, setData] = useState<PlayerRequestLogPage | null>(null)
  const [selected, setSelected] = useState<PlayerRequestLog | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const updateParams = (updates: Record<string, string | undefined>) => {
    const next = new URLSearchParams(params)
    Object.entries(updates).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key))
    setParams(next)
  }

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    return adminAPI.playerRequestLogs({ month, page, page_size: PAGE_SIZE, path: path || undefined, method: method || undefined, status })
      .then(setData)
      .catch(() => setError('播放器日志加载失败。'))
      .finally(() => setLoading(false))
  }, [method, month, page, path, status])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setPathDraft(path) }, [path])
  useEffect(() => { setStatusDraft(status ? String(status) : '') }, [status])
  useEffect(() => {
    const next = new URLSearchParams(params)
    let changed = false
    for (const [key, value] of [['month', month], ['method', method], ['page', page === 1 ? '' : String(page)], ['status', status ? String(status) : ''], ['path', path]] as const) {
      const values = params.getAll(key)
      const current = values[0] ?? ''
      if (values.length > 1 || current !== value) {
        if (value) next.set(key, value)
        else next.delete(key)
        changed = true
      }
    }
    if (changed) setParams(next, { replace: true })
  }, [method, month, page, params, path, setParams, status])
  useEffect(() => () => { if (refreshTimer.current) clearTimeout(refreshTimer.current) }, [])

  const scheduleRefresh = useCallback(() => {
    if (refreshTimer.current) clearTimeout(refreshTimer.current)
    refreshTimer.current = setTimeout(() => { void load() }, 150)
  }, [load])
  const onEvent = useCallback((event: { type: string }) => {
    if (event.type === 'player_request_log_changed') scheduleRefresh()
  }, [scheduleRefresh])
  useSSE(onEvent, { onOpen: scheduleRefresh })

  const pages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))
  const rows = data?.items ?? []
  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3"><ScrollText className="h-7 w-7 text-brand-500" /><div><h1 className="page-heading">播放器日志</h1><p className="page-subtitle">实时查看播放器兼容 API 的脱敏请求记录。</p></div></header>

      <form className="glass-panel grid gap-3 md:grid-cols-[10rem_9rem_8rem_minmax(0,1fr)_auto]" onSubmit={(event) => { event.preventDefault(); const parsedStatus = positiveInt(statusDraft, 0); updateParams({ path: pathDraft.trim() || undefined, status: parsedStatus >= 100 && parsedStatus <= 599 ? String(parsedStatus) : undefined, page: undefined }) }}>
        <label className="text-xs text-ink-50">月份<input className="input-base mt-1" type="month" value={month} onChange={(event) => updateParams({ month: event.target.value, page: undefined })} /></label>
        <label className="text-xs text-ink-50">方法<select className="input-base mt-1" value={method} onChange={(event) => updateParams({ method: event.target.value || undefined, page: undefined })}>{methods.map((value) => <option key={value || 'all'} value={value}>{value || '全部'}</option>)}</select></label>
        <label className="text-xs text-ink-50">状态码<input className="input-base mt-1" type="number" min="100" max="599" value={statusDraft} placeholder="全部" onChange={(event) => setStatusDraft(event.target.value)} /></label>
        <label className="text-xs text-ink-50">路径<input className="input-base mt-1" value={pathDraft} maxLength={512} placeholder="筛选路由路径" onChange={(event) => setPathDraft(event.target.value)} /></label>
        <div className="flex items-end gap-2"><button type="submit" className="btn-primary">筛选</button><button type="button" className="icon-btn" aria-label="刷新" title="刷新" onClick={() => void load()}><RefreshCw size={17} /></button></div>
      </form>

      <section className="glass-panel !p-0">
        {loading && !data ? <p className="py-12 text-center text-ink-50">加载中...</p> : error ? <p className="py-12 text-center text-red-500">{error}</p> : rows.length === 0 ? <p className="py-12 text-center text-ink-50">当前条件下暂无播放器请求。</p> : (
          <>
            <div className="hidden overflow-x-auto lg:block"><table className="data-table min-w-[900px]"><thead><tr><th>时间</th><th>方法</th><th>路由</th><th>状态</th><th>耗时</th><th>IP</th></tr></thead><tbody>{rows.map((log) => <tr key={`${log.id}-${log.requested_at}`} className="cursor-pointer" tabIndex={0} onClick={() => setSelected(log)} onKeyDown={(event) => { if (event.key === 'Enter') setSelected(log) }}><td className="whitespace-nowrap">{new Date(log.requested_at).toLocaleString()}</td><td className="font-mono">{log.method}</td><td className="max-w-md break-all font-mono text-xs">{log.route}</td><td><span className={statusClass(log.status)}>{log.status}</span></td><td>{log.duration_ms} ms</td><td className="font-mono text-xs">{log.ip || '-'}</td></tr>)}</tbody></table></div>
            <div className="divide-y divide-[var(--app-border)] lg:hidden">{rows.map((log) => <button type="button" key={`${log.id}-${log.requested_at}`} className="block w-full p-4 text-left" onClick={() => setSelected(log)}><div className="flex items-center justify-between gap-3"><span className="font-mono text-sm font-semibold text-ink-600">{log.method}</span><span className={statusClass(log.status)}>{log.status}</span></div><p className="mt-2 break-all font-mono text-xs text-ink-600">{log.route}</p><p className="mt-2 text-xs text-ink-50">{new Date(log.requested_at).toLocaleString()} · {log.duration_ms} ms · {log.ip || '-'}</p></button>)}</div>
          </>
        )}
      </section>

      <footer className="flex items-center justify-between text-sm text-ink-50"><span>共 {data?.total ?? 0} 条</span><div className="flex items-center gap-2"><button type="button" className="icon-btn" aria-label="上一页" title="上一页" disabled={page <= 1} onClick={() => updateParams({ page: String(page - 1) })}><ChevronLeft size={17} /></button><span>{page} / {pages}</span><button type="button" className="icon-btn" aria-label="下一页" title="下一页" disabled={page >= pages} onClick={() => updateParams({ page: String(page + 1) })}><ChevronRight size={17} /></button></div></footer>
      {selected && <LogDetails log={selected} onClose={() => setSelected(null)} />}
    </div>
  )
}
