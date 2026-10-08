import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { Download } from 'lucide-react'
import { huangguoaiAPI, type HuangGuoAIDownload, type HuangGuoAIDownloadConfig, type HuangGuoAIDownloadWork } from '../api/huangguoai'
import { ModalShell } from '../components/ModalShell'
import { DownloadProgress } from '../components/DownloadProgress'
import { statusLabels as baseStatusLabels, statusColors as baseStatusColors } from '../utils/downloadStatus'
import { Select } from '../components/Select'

import { huangGuoAIReviewURL } from '../api/client'

const statusLabels = { ...baseStatusLabels, pending_review: '⚠ 待人工确认' }
const statusColors = { ...baseStatusColors, pending_review: 'text-gold-600 bg-gold-500/10' }
const labels: Record<string, string> = statusLabels
export function HuangGuoAIDownloadSpace() {
  const [params, setParams] = useSearchParams()
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 1000000 ? rawPage : 1
  const rawStatus = params.get('status') ?? 'downloading'
  const status = labels[rawStatus] ? rawStatus : ''
  const keyword = (params.get('keyword') ?? '').trim()
  const [search, setSearch] = useState(keyword)
  useEffect(() => { setSearch(keyword) }, [keyword])
  const [config, setConfig] = useState<HuangGuoAIDownloadConfig | null>(null)
  const [configError, setConfigError] = useState(false)
  const [result, setResult] = useState<{ items: HuangGuoAIDownloadWork[]; total: number; page: number; status: string; keyword: string } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [busy, setBusy] = useState(false)
  const [settings, setSettings] = useState(false)
  const settingsButton = useRef<HTMLButtonElement>(null)
  const closeSettings = () => { setSettings(false); settingsButton.current?.focus() }
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  useEffect(() => { const next = new URLSearchParams(params); next.set('page', String(page)); next.set('status', status); if (keyword) next.set('keyword', keyword); else next.delete('keyword'); if (next.toString() !== params.toString()) setParams(next, { replace: true }) }, [params, setParams, page, status, keyword])
  useEffect(() => {
    const controller = new AbortController()
    void huangguoaiAPI.downloadConfig(controller.signal).then(data => { if (!controller.signal.aborted) { setConfig(data); setConfigError(false) } }).catch(() => { if (!controller.signal.aborted) setConfigError(true) })
    return () => controller.abort()
  }, [retry])
  useEffect(() => {
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>; let loading = false
    const load = async () => { if (controller.signal.aborted || document.hidden || loading) return; loading = true; clearTimeout(timer)
      try { const data = await huangguoaiAPI.downloadWorks(page, status, keyword, controller.signal); if (!controller.signal.aborted) { if (page > 1 && data.total <= (page - 1) * 50) setParams(previous => { const next = new URLSearchParams(previous); next.set('page', String(Math.max(1, Math.ceil(data.total / 50)))); return next }, { replace: true }); setResult({ ...data, page, status, keyword }); setError(false) } } catch { if (!controller.signal.aborted) setError(true) }
      finally { loading = false; if (!controller.signal.aborted && !document.hidden) timer = setTimeout(() => void load(), 5000) }
    }
    const visible = () => { clearTimeout(timer); if (!document.hidden) void load() }; document.addEventListener('visibilitychange', visible); void load()
    return () => { controller.abort(); clearTimeout(timer); document.removeEventListener('visibilitychange', visible) }
  }, [page, status, keyword, retry, setParams])
  const perform = async (id: string, action: 'retry' | 'cancel' | 'confirm', work = false, reviewToken = '') => {
    if (busy) return
    if (action === 'confirm' && !window.confirm('严格校验未通过。请确认已检查画面、声音和内容完整性；保留校验警告并发布，之后可整理入库。是否继续？')) return
    setBusy(true)
    try {
      if (work && action !== 'confirm') {
        const value = await huangguoaiAPI.downloadWorkAction(id, action)
        if (active.current) toast.success(action === 'retry' ? `已重新入队 ${value.updated} 个下载任务` : `已取消 ${value.updated} 个未完成下载任务`)
      } else {
        if (action === 'confirm') await huangguoaiAPI.confirmDownload(id, reviewToken)
        else await huangguoaiAPI.downloadAction(id, action)
        if (active.current) toast.success(action === 'confirm' ? '已确认，等待发布并整理入库' : action === 'retry' ? '下载任务已重新入队' : '下载任务已取消')
      }
      if (active.current) setRetry(v => v + 1)
    } catch { if (active.current) toast.error('操作失败，旧执行尚未退出时请稍后重试') }
    finally { if (active.current) setBusy(false) }
  }
  const navigate = (changes: Record<string, string>) => { const next = new URLSearchParams(params); for (const [key, value] of Object.entries(changes)) next.set(key, value); setParams(next) }
  return <section className="space-y-6">
    <div className="flex flex-wrap items-center gap-3">
      <form role="search" aria-label="搜索下载作品" className="flex min-w-0 flex-1 basis-64 items-center gap-2" onSubmit={event => { event.preventDefault(); const value = search.trim(); setSearch(value); navigate({ keyword: value, page: '1' }) }}>
        <input type="search" aria-label="搜索作品" placeholder="搜索作品名称或 ID" className="input-field min-w-0 flex-1" value={search} onChange={event => setSearch(event.target.value)} />
        <button className="btn-outline" type="submit">搜索</button>
        {(search || keyword) && <button className="btn-outline" type="button" onClick={() => { setSearch(''); navigate({ keyword: '', page: '1' }) }}>清空搜索</button>}
      </form>
      <div className="flex items-center gap-2 text-sm"><span>作品状态</span><Select className="input-field min-w-44" aria-label="黄果AI下载状态" value={status} onChange={value => navigate({ status: value, page: '1' })}><option value="">全部状态</option>{Object.entries(labels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select></div>
      <div className="flex flex-wrap items-center gap-2"><Link className="btn-primary" to="/discover?system=huangguoai">去发现下载</Link><button className="btn-outline" onClick={() => setRetry(v => v + 1)}>刷新</button><button ref={settingsButton} className="btn-outline" onClick={() => setSettings(true)}>设置</button></div>
    </div>
    {config && !config.root && <p className="text-sm text-ink-50">尚未设置下载目录，请点击“设置”配置。</p>}
    {(error || configError) && <p role="alert">下载信息读取失败 <button className="btn-outline" onClick={() => setRetry(v => v + 1)}>重试</button></p>}
    <section className="space-y-3" aria-label="黄果AI下载任务">
      {!result || result.page !== page || result.status !== status || result.keyword !== keyword ? <p role="status">读取下载队列…</p> : <>
        {result.items.map(work => <HuangGuoAIDownloadWork key={work.source_id} {...{ work, busy, perform, refresh: retry }} />)}
        {!error && result.items.length === 0 && <div className="card flex min-h-64 flex-col items-center justify-center gap-4 px-4 py-10 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-brand-500/10 text-brand-500"><Download size={24} aria-hidden="true" /></div>
          <div className="space-y-2"><h3 className="font-semibold">{keyword ? '没有找到匹配的下载作品' : status === 'downloading' ? '暂无正在下载的作品' : status ? '当前状态暂无下载作品' : '还没有下载任务'}</h3><p className="text-sm text-ink-50">{keyword ? '请尝试其他作品名称或 ID，或清空搜索后查看。' : status ? '可以查看全部任务，或前往发现页添加下载。' : '从发现页打开作品详情，即可发起下载。'}</p></div>
          <div className="flex flex-wrap justify-center gap-2">{status && <button className="btn-outline" onClick={() => navigate({ status: '', page: '1' })}>查看全部</button>}<Link className="btn-primary" to="/discover?system=huangguoai">去发现下载</Link></div>
        </div>}
        {result.total > 0 && <div className="flex flex-wrap items-center gap-3"><button className="btn-outline" disabled={page === 1} onClick={() => navigate({ page: String(page - 1) })}>上一页</button><span>第 {page} 页 · 共 {result.total} 部</span><button className="btn-outline" disabled={page * 50 >= result.total} onClick={() => navigate({ page: String(page + 1) })}>下一页</button></div>}
      </>}
    </section>
    {settings && config && <HuangGuoAIDownloadSettings initial={config} onClose={closeSettings} onSaved={() => { closeSettings(); setRetry(v => v + 1); toast.success('下载设置已保存') }} />}
    {settings && !config && <p role="alert">设置读取失败，请重试加载。</p>}
  </section>
}

function HuangGuoAIDownloadSettings({ initial, onClose, onSaved }: { initial: HuangGuoAIDownloadConfig; onClose: () => void; onSaved: () => void }) {
  const [config, setConfig] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  const dirty = config.root !== initial.root || config.concurrency !== initial.concurrency || config.verification_concurrency !== initial.verification_concurrency
  return <ModalShell ariaLabel="黄果AI下载设置" maxWidth="max-w-xl" className="max-h-[85dvh] overflow-y-auto p-5" onClose={busy || dirty ? undefined : onClose}>
    <form className="space-y-4" onKeyDown={event => {
      if (event.key !== 'Tab') return
      const controls = event.currentTarget.querySelectorAll<HTMLElement>('input:not(:disabled), button:not(:disabled)')
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }} onSubmit={event => {
      event.preventDefault(); if (busy) return; setBusy(true)
      void huangguoaiAPI.saveDownloadConfig(config).then(() => { if (active.current) onSaved() }).catch(() => { if (active.current) setError('保存失败，请检查目录及并发配置。') }).finally(() => { if (active.current) setBusy(false) })
    }}>
      <h2 className="text-lg font-semibold">下载设置</h2>
      <label className="block">下载存储根目录<input autoFocus required disabled={busy} className="input-field mt-2 w-full" value={config.root} onChange={event => setConfig({ ...config, root: event.target.value })} /></label>
      <label className="block">并发下载数量<input required disabled={busy} type="number" min={1} max={10} step={1} className="input-field mt-2 w-full" value={config.concurrency} onChange={event => setConfig({ ...config, concurrency: Number(event.target.value) })} /></label>
      <label className="block">并发校验数量<input required disabled={busy} type="number" min={1} max={20} step={1} className="input-field mt-2 w-full" value={config.verification_concurrency} onChange={event => setConfig({ ...config, verification_concurrency: Number(event.target.value) })} /></label>
      <p className="text-sm text-ink-50">下载与校验分别使用并发名额。保存后动态生效，调低不会中断正在处理的下载。严格校验通过后发布文件；校验失败会保留候选，须管理员试播并明确确认后才能发布。</p>
      {initial.root && <dl className="space-y-2 text-sm"><div><dt className="text-ink-50">临时下载目录 · 不要备份</dt><dd className="break-all">{initial.temporary_dir}</dd></div><div><dt className="text-ink-50">完成输出目录 · 只备份此目录</dt><dd className="break-all">{initial.output_dir}</dd></div></dl>}
      <p className="text-sm text-ink-50">临时目录和完成目录需在同一文件系统。修改根目录只影响首次下载的新作品，已有作品补集沿用原位置。项目不判断云盘上传结果，不自动清理已完成视频。</p>
      {error && <p role="alert">{error}</p>}
      <div className="flex gap-3"><button className="btn-primary" disabled={busy}>{busy ? '保存中…' : '保存设置'}</button><button type="button" className="btn-outline" disabled={busy} onClick={onClose}>取消</button></div>
    </form>
  </ModalShell>
}
function HuangGuoAIDownloadWork({ work, refresh, busy, perform }: { work: HuangGuoAIDownloadWork; refresh: number; busy: boolean; perform: (id: string, action: 'retry' | 'cancel' | 'confirm', work?: boolean, reviewToken?: string) => Promise<void> }) {
  const movie = work.kind === 'movie'
  const series = work.kind === 'series'
  const unit = movie ? '部' : series ? '集' : '个任务'
  const itemLabel = movie ? '电影' : series ? '分集' : '下载项'
  const [open, setOpen] = useState(false)
  const [previewID, setPreviewID] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [data, setData] = useState<{ items: HuangGuoAIDownload[]; total: number; page: number } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!open) return
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>; let loading = false
    const load = async () => {
      if (controller.signal.aborted || document.hidden || loading) return
      loading = true; clearTimeout(timer)
      try {
        const value = await huangguoaiAPI.downloadEpisodes(work.source_id, page, controller.signal)
        if (!controller.signal.aborted) {
          if (page > 1 && value.total <= (page - 1) * 50) { setPage(Math.max(1, Math.ceil(value.total / 50))); return }
          setData({ ...value, page }); setError(false)
        }
      } catch { if (!controller.signal.aborted) setError(true) }
      finally { loading = false; if (!controller.signal.aborted && !document.hidden) timer = setTimeout(() => void load(), 5000) }
    }
    const visible = () => { clearTimeout(timer); if (!document.hidden) void load() }
    document.addEventListener('visibilitychange', visible); void load()
    return () => { controller.abort(); clearTimeout(timer); document.removeEventListener('visibilitychange', visible) }
  }, [open, page, refresh, retry, work.source_id])
  return <article className="card relative space-y-2 p-3 sm:p-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <button className="min-h-11 w-full min-w-0 flex-none break-words sm:w-auto sm:flex-1 text-left font-semibold focus-visible:outline-brand-500" aria-expanded={open} aria-controls={`hga-episodes-${work.source_id}`} onClick={() => setOpen(v => !v)}>{open ? '▾' : '▸'} {work.title}</button><span className="text-xs text-ink-50">{movie ? '电影' : series ? '剧集' : '未分类'}</span>
      {work.total > 0 && work.completed === work.total && <span className="rounded-full bg-emerald-500/10 px-2 py-1 text-xs font-semibold text-emerald-600" title="当前已创建的下载任务全部完成">✅ 全部完成</span>}
      {work.failed + work.cancelled > 0 && <button className="btn-outline" disabled={busy} onClick={() => void perform(work.source_id, 'retry', true)}>重试失败及取消任务</button>}
      {work.completed < work.total && <button className="btn-outline" disabled={busy} onClick={() => void perform(work.source_id, 'cancel', true)}>取消未完成</button>}
    </div>
    <div className="flex flex-wrap items-center gap-2 text-xs"><span>完成 {work.completed}/{work.total} {unit}</span>{(Object.keys(statusLabels) as HuangGuoAIDownload['status'][]).filter(status => work[status] > 0).map(status => <span key={status} className={`rounded-full px-2 py-1 ${statusColors[status]}`}>{statusLabels[status]} {work[status]}</span>)}</div>
    <DownloadProgress label="作品完成进度" value={work.completed} total={work.total} />
    <div id={`hga-episodes-${work.source_id}`} hidden={!open} className="border-l-2 border-brand-500/30 pl-3">
      {error && <p role="alert">{itemLabel}读取失败 <button className="btn-outline" onClick={() => setRetry(v => v + 1)}>重试加载</button></p>}
      {open && (data?.page !== page ? <p role="status">加载{itemLabel}…</p> : <>
        {data.items.map(row => <div key={row.id} data-download-episode={row.episode} className="relative border-b border-ink-100/10">
          <div className="flex min-h-11 items-center gap-2 text-xs">
            <Link className="shrink-0 py-3 font-semibold" to={`/discover?system=huangguoai&id=${encodeURIComponent(work.source_id)}`}>{movie ? '正片' : series ? `E${String(row.episode).padStart(3, '0')}` : '下载项'}</Link>
            <span className="hidden min-w-0 flex-1 truncate text-ink-50 sm:block" title={row.relative_path}>{row.relative_path}</span>
            <span className={`shrink-0 rounded px-1.5 py-1 ${statusColors[row.status]}`}>{statusLabels[row.status]}</span>
            {row.status === 'downloading' && <span className="shrink-0 tabular-nums text-ink-50">{row.total_bytes > 0 ? `${Math.max(0, Math.min(100, Math.floor(row.bytes * 100 / row.total_bytes)))}%` : `${(row.bytes / 1048576).toFixed(1)} MB`}</span>}
            <details className="relative ml-auto shrink-0"><summary className="cursor-pointer px-2 py-3 text-ink-50" aria-label={series ? `查看第 ${row.episode} 集任务详情` : `查看${itemLabel}任务详情`}>详情</summary><div className="absolute right-0 top-full z-10 max-h-80 w-64 max-w-[70vw] space-y-2 overflow-y-auto rounded-lg border border-ink-100/10 bg-[var(--app-bg)] p-3 shadow-xl">
              <p className="break-all">{row.relative_path || '输出路径尚未记录'}</p><p>尝试次数：{row.attempts}</p>
              <p>已下载 {(row.bytes / 1048576).toFixed(1)} MB{row.total_bytes > 0 ? ` / ${(row.total_bytes / 1048576).toFixed(1)} MB` : ' · 总大小未知'}</p>
              {row.warning && <p className="break-words text-gold-600">校验警告：{row.warning}</p>}
              {row.confirmed_at && <p>管理员已确认保留 · {new Date(row.confirmed_at).toLocaleString()}</p>}
              {row.error && <p className="break-words text-red-500">{row.error}</p>}
            </div></details>
            {row.status === 'pending_review' && <button className="shrink-0 px-2 py-3 font-semibold text-brand-500" onClick={() => setPreviewID(previewID === `${row.id}:${row.review_token}` ? null : `${row.id}:${row.review_token}`)}>试播</button>}
            {['failed', 'cancelled', 'pending_review'].includes(row.status) ? <button className="shrink-0 px-2 py-3 font-semibold text-brand-500 disabled:opacity-50" disabled={busy} onClick={() => void perform(row.id, 'retry')}>重试</button> : row.status !== 'completed' && <button className="shrink-0 px-2 py-3 text-ink-50 disabled:opacity-50" aria-label={series ? `取消第 ${row.episode} 集下载` : `取消${itemLabel}下载`} disabled={busy} onClick={() => void perform(row.id, 'cancel')}>取消下载</button>}
          </div>
          {previewID === `${row.id}:${row.review_token}` && row.status === 'pending_review' && <div className="space-y-3 pb-3">
            <p className="text-sm text-gold-600">校验未通过：{row.warning}。能播放不代表内容完整，请检查画面、声音及结尾。</p>
            <video controls playsInline preload="metadata" className="max-h-[60dvh] w-full bg-black" src={huangGuoAIReviewURL(row.id, row.review_token)} aria-label="待确认文件试播" />
            <div className="flex flex-wrap gap-2"><button className="btn-primary" disabled={busy} onClick={() => void perform(row.id, 'confirm', false, row.review_token)}>保留并发布</button><button className="btn-outline" disabled={busy} onClick={() => void perform(row.id, 'cancel')}>删除候选并取消</button><button className="btn-outline" onClick={() => setPreviewID(null)}>关闭试播</button></div>
          </div>}
          {row.status === 'downloading' && <DownloadProgress label={`${itemLabel}下载进度`} value={row.bytes} total={row.total_bytes} />}
        </div>)}
        {data.total > 50 && <div className="flex flex-wrap items-center gap-3 pt-3"><button className="btn-outline" disabled={page === 1} onClick={() => setPage(v => v - 1)}>上一页{itemLabel}</button><span>{itemLabel}第 {page} 页 · 共 {data.total} {unit}</span><button className="btn-outline" disabled={page * 50 >= data.total} onClick={() => setPage(v => v + 1)}>下一页{itemLabel}</button></div>}
      </>)}
    </div>
  </article>
}
