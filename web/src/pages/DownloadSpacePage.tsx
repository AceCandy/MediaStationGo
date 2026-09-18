import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { hongguoDownloadsAPI, type DownloadConfig, type HongGuoDownload, type HongGuoDownloadWork } from '../api/hongguoDownloads'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import { HongGuoDownloadActions } from './HongGuoDownloadActions'

const sourceLabels: Record<string, string> = { app: 'App 接口', fallback: '备用接口', official: '官方网页' }
const statusLabels: Record<HongGuoDownload['status'], string> = { downloading: '↓ 下载中', verifying: '◉ 校验中', publishing: '↗ 发布中', waiting_verify: '◷ 等待校验', failed: '⚠ 失败', queued: '◷ 等待下载', cancelled: '⊘ 已取消', completed: '✓ 已完成' }
const statusColors: Record<HongGuoDownload['status'], string> = { downloading: 'text-brand-500 bg-brand-500/10', verifying: 'text-sage-600 bg-sage-500/10', publishing: 'text-sage-600 bg-sage-500/10', waiting_verify: 'text-gold-600 bg-gold-500/10', failed: 'text-red-500 bg-red-500/10', queued: 'text-ink-50 bg-ink-100/5', cancelled: 'text-ink-50 bg-ink-100/5', completed: 'text-emerald-600 bg-emerald-500/10' }
const message = (err: unknown) => (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '操作失败，请重试'

export function DownloadSpacePage() {
  const key = useMediaAccessKey()
  return <DownloadSpaceContent key={key} />
}

function DownloadSpaceContent() {
  const [params, setParams] = useSearchParams()
  const raw = Number(params.get('page') ?? 1)
  const page = Number.isInteger(raw) && raw >= 1 && raw <= 1000000 ? raw : 1
  const failedOnly = params.get('failed_only') === 'true'
  const [config, setConfig] = useState<DownloadConfig | null>(null)
  const [root, setRoot] = useState('')
  const [concurrency, setConcurrency] = useState('')
  const [verificationConcurrency, setVerificationConcurrency] = useState('')
  const [fullVerification, setFullVerification] = useState(true)
  const [hardwareVerification, setHardwareVerification] = useState(false)
  const [priority, setPriority] = useState('')
  const dirty = root !== (config?.root ?? '') || concurrency !== String(config?.concurrency ?? '') || verificationConcurrency !== String(config?.verification_concurrency ?? '') || fullVerification !== (config?.full_verification ?? true) || hardwareVerification !== (config?.hardware_verification ?? false) || priority !== (config?.priority ?? '')
  const [configError, setConfigError] = useState('')
  const [result, setResult] = useState<{ page: number; failedOnly: boolean; items: HongGuoDownloadWork[]; total: number } | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const settingsButton = useRef<HTMLButtonElement>(null)
  const closeSettings = () => { setSettingsOpen(false); settingsButton.current?.focus() }
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  const [configRevision, setConfigRevision] = useState(0)
  const [busy, setBusy] = useState('')
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  useEffect(() => {
    if (raw === page && params.getAll('page').length <= 1 && (failedOnly ? params.getAll('failed_only').length === 1 : !params.has('failed_only'))) return
    const next = new URLSearchParams(params); next.set('page', String(page)); if (failedOnly) next.set('failed_only', 'true'); else next.delete('failed_only'); setParams(next, { replace: true })
  }, [params, setParams, raw, page, failedOnly])
  useEffect(() => {
    const controller = new AbortController()
    void hongguoDownloadsAPI.config(controller.signal).then((value) => {
      if (!controller.signal.aborted) { setConfig(value); setRoot(value.root); setConcurrency(String(value.concurrency)); setVerificationConcurrency(String(value.verification_concurrency)); setFullVerification(value.full_verification); setHardwareVerification(value.hardware_verification); setPriority(value.priority); setConfigError('') }
    }).catch((err) => { if (!controller.signal.aborted) setConfigError(message(err)) })
    return () => controller.abort()
  }, [configRevision])
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const load = async () => {
      try {
        const value = await hongguoDownloadsAPI.works(page, failedOnly, controller.signal)
        if (!controller.signal.aborted) {
          if (page > 1 && value.total <= (page - 1) * 50) {
            setParams((previous) => { const next = new URLSearchParams(previous); next.set('page', String(Math.max(1, Math.ceil(value.total / 50)))); return next }, { replace: true })
          }
          setResult({ ...value, page, failedOnly }); setError('')
        }
      } catch (err) { if (!controller.signal.aborted) setError(message(err)) }
      if (!controller.signal.aborted) timer = setTimeout(() => void load(), 5000)
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [page, failedOnly, refresh, setParams])
  const perform = async (id: string, action: 'cancel' | 'retry' | 'work') => {
    if (busy) return
    setBusy(id)
    try {
      if (action === 'work') {
        const value = await hongguoDownloadsAPI.retryWork(id)
        if (active.current) toast.success(`已重新入队 ${value.added} 集${value.skipped ? `，${value.skipped} 集缺少资料，未重试` : ''}`)
      } else await hongguoDownloadsAPI.action(id, action)
      if (active.current) setRefresh((v) => v + 1)
    }
    catch (err) { if (active.current) toast.error(message(err)) }
    finally { if (active.current) setBusy('') }
  }
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('page', String(value)); setParams(next) }
  return <div className="space-y-6">
    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200">
      <nav aria-label="下载来源"><button type="button" aria-pressed="true" className="min-h-11 border-b-2 border-brand-500 px-4 py-3 text-sm font-semibold text-brand-500 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500">红果短剧</button></nav>
      <div className="flex flex-wrap gap-2"><HongGuoDownloadActions /><button ref={settingsButton} className="btn-outline" onClick={() => { setRoot(config?.root ?? ''); setConcurrency(String(config?.concurrency ?? '')); setVerificationConcurrency(String(config?.verification_concurrency ?? '')); setFullVerification(config?.full_verification ?? true); setHardwareVerification(config?.hardware_verification ?? false); setPriority(config?.priority ?? ''); setSettingsOpen(true) }}>设置</button></div>
    </div>
    {config && !config.root && <p className="text-sm text-ink-50">尚未设置下载目录，请点击“设置”配置。</p>}
    {settingsOpen && <ModalShell ariaLabel="红果下载设置" maxWidth="max-w-xl" className="max-h-[85dvh] overflow-y-auto p-5" onClose={busy === 'config' || dirty ? undefined : closeSettings}>
    <section className="space-y-4" onKeyDown={(event) => {
      if (event.key === 'Escape' && event.defaultPrevented) { event.stopPropagation(); return }
      if (event.key !== 'Tab') return
      const controls = event.currentTarget.querySelectorAll<HTMLElement>('input:not(:disabled), button:not(:disabled)')
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }}>
      <h2 className="text-lg font-semibold">下载设置</h2>
      {configError && <p role="alert">{configError} <button className="btn-outline" onClick={() => setConfigRevision((v) => v + 1)}>重试</button></p>}
      <form className="space-y-3" onSubmit={(event) => {
        event.preventDefault(); if (busy) return; setBusy('config')
        void hongguoDownloadsAPI.save({ root, concurrency: Number(concurrency), verification_concurrency: Number(verificationConcurrency), full_verification: fullVerification, hardware_verification: hardwareVerification, priority }).then((value) => { if (active.current) { setConfig(value); setRoot(value.root); setConcurrency(String(value.concurrency)); setVerificationConcurrency(String(value.verification_concurrency)); setFullVerification(value.full_verification); setHardwareVerification(value.hardware_verification); setPriority(value.priority); closeSettings(); toast.success('下载设置已保存') } }).catch((err) => { if (active.current) toast.error(message(err)) }).finally(() => { if (active.current) setBusy('') })
      }}>
        <label className="block">下载存储根目录<input autoFocus className="input-field mt-2 w-full" value={root} onChange={(e) => setRoot(e.target.value)} placeholder="例如 /downloads/hongguo" required disabled={!config || !!busy} /></label>
        <label className="block">并发下载数量<input type="number" min={1} max={10} step={1} required className="input-field mt-2 w-full" value={concurrency} onChange={(e) => setConcurrency(e.target.value)} disabled={!config || !!busy} /></label>
        <label className="block">并发校验数量<input type="number" min={1} max={20} step={1} required className="input-field mt-2 w-full" value={verificationConcurrency} onChange={(e) => setVerificationConcurrency(e.target.value)} disabled={!config || !!busy} /></label>
        <label className="flex min-h-11 items-center gap-2"><input type="checkbox" checked={fullVerification} onChange={(event) => setFullVerification(event.target.checked)} disabled={!config || !!busy} />完整解码校验</label>
        <p className="text-sm text-ink-50">默认开启，逐帧检查音视频是否可解码。关闭可缩短处理时间，仍保留下载完整性、解密封装、轨道和时长检查；可能无法提前发现中途损坏。保存后用于后续校验，正在校验的分集不受影响。</p>
        <label className="flex min-h-11 items-center gap-2"><input type="checkbox" checked={hardwareVerification} onChange={(event) => setHardwareVerification(event.target.checked)} disabled={!config || !!busy || !fullVerification} />启用核显加速校验（VAAPI）</label>
        <p className="text-sm text-ink-50">默认关闭，仅加速解码检查，不转码。需 FFmpeg 支持 VAAPI，且服务可访问 /dev/dri/renderD128；硬解报错自动回退软件校验。保存后用于后续校验，无需重启；硬件与软件的损坏检出能力可能不同。</p>
        <p className="text-sm text-ink-50">下载支持同时处理 1–10 集，校验支持 1–20 集，互不占用名额。保存后动态生效，无需重启；调低不会中断正在处理的分集，后续按新上限领取。机械盘建议先试 2–3 个校验；等待校验的文件会暂存在临时目录。</p>
        <div className="space-y-2"><p>下载接口优先级</p><Select aria-label="下载接口优先级" className="input-field w-full" value={priority} onChange={setPriority} disabled={!config || !!busy}><option value="app">App → 备用 → 官方网页（推荐）</option><option value="fallback">备用 → App → 官方网页</option><option value="official">官方网页 → App → 备用</option></Select></div>
        <p className="text-sm text-ink-50">自动选择所用接口提供的最高可用画质，不跨接口比较。接口顺序用于后续取流，已开始传输的分集不受影响。</p>
        <button className="btn-primary" disabled={!config || !!busy}>{busy === 'config' ? '保存中…' : '保存设置'}</button>
        <button type="button" className="btn-outline ml-2" disabled={busy === 'config'} onClick={closeSettings}>取消</button>
      </form>
      {config?.root && <dl className="space-y-2 text-sm"><div><dt className="text-ink-50">临时下载目录 · 不要备份</dt><dd className="break-all">{config.temporary_dir}</dd></div><div><dt className="text-ink-50">完成输出目录 · CD2 只备份此目录</dt><dd className="break-all">{config.output_dir}</dd></div></dl>}
      <p className="text-sm text-ink-50">按红果上线年月归档：年/月/作品；缺月份放“未知月份”，缺年份放“未知年份/作品”。补集沿用原位置，修改目录只影响首次下载的新作品。</p>
      <p className="text-sm text-ink-50">临时目录和完成目录需在同一文件系统。项目不判断云盘上传结果，不自动清理已完成视频。</p>
    </section></ModalShell>}
    <section className="space-y-3" aria-label="红果下载任务">
      <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-semibold">作品任务</h2><Link className="btn-outline" to="/discover?system=hongguo">去红果发现下载</Link></div>
      <label className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" checked={failedOnly} onChange={(event) => { const next = new URLSearchParams(params); if (event.target.checked) next.set('failed_only', 'true'); else next.delete('failed_only'); next.set('page', '1'); setParams(next) }} />仅显示含失败集的剧集</label>
      {error && <p role="alert">{error}</p>}
      {result?.page !== page || result.failedOnly !== failedOnly ? <p role="status">加载中…</p> : <>
        {result.items.length === 0 && <p className="text-ink-50">{failedOnly ? '暂无含失败集的剧集。' : '暂无下载任务。从红果发现打开作品详情后发起下载。'}</p>}
        {result.items.map((work) => <DownloadWork key={work.source_id} work={work} refresh={refresh} busy={busy} perform={perform} />)}
        <div className="flex flex-wrap items-center gap-3"><button className="btn-outline" disabled={page <= 1} onClick={() => goPage(page - 1)}>上一页</button><span>第 {page} 页 · 共 {result.total} 部</span><button className="btn-outline" disabled={page * 50 >= result.total} onClick={() => goPage(page + 1)}>下一页</button></div>
      </>}
    </section>
  </div>
}

function DownloadWork({ work, refresh, busy, perform }: { work: HongGuoDownloadWork; refresh: number; busy: string; perform: (id: string, action: 'cancel' | 'retry' | 'work') => Promise<void> }) {
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [result, setResult] = useState<{ items: HongGuoDownload[]; total: number; page: number } | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const load = async () => {
      try {
        const value = await hongguoDownloadsAPI.episodes(work.source_id, page, controller.signal)
        if (!controller.signal.aborted) { setResult({ ...value, page }); setError('') }
      } catch (err) { if (!controller.signal.aborted) setError(message(err)) }
      if (!controller.signal.aborted) timer = setTimeout(() => void load(), 5000)
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [open, page, refresh, work.source_id])
  return <article className="card relative space-y-2 p-3 sm:p-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <button className="min-h-11 min-w-0 flex-1 break-words text-left font-semibold focus-visible:outline-brand-500" aria-expanded={open} aria-controls={`episodes-${work.source_id}`} onClick={() => setOpen((value) => !value)}>{open ? '▾' : '▸'} {work.title}</button>
      {work.total > 0 && work.completed === work.total && <span className="rounded-full bg-emerald-500/10 px-2 py-1 text-xs font-semibold text-emerald-600" title="当前已创建的下载任务全部完成">✅ 全部完成</span>}
      {work.failed > 0 && <button className="btn-outline" disabled={!!busy} onClick={() => void perform(work.source_id, 'work')}>重试失败集</button>}
    </div>
    <div className="flex flex-wrap items-center gap-2 text-xs"><span>完成 {work.completed}/{work.total} 集</span>{(Object.keys(statusLabels) as HongGuoDownload['status'][]).filter((status) => work[status] > 0).map((status) => <span key={status} className={`rounded-full px-2 py-1 ${statusColors[status]}`}>{statusLabels[status]} {work[status]}</span>)}</div>
    <DownloadProgress label="剧集完成进度" value={work.completed} total={work.total} />
    <div id={`episodes-${work.source_id}`} hidden={!open} className="border-l-2 border-brand-500/30 pl-3">
      {error && <p role="alert">{error}</p>}
      {open && (result?.page !== page ? <p role="status">加载分集…</p> : <>
        {result.items.map((row) => <div key={row.id} data-download-episode={row.episode} className="relative border-b border-ink-100/10">
          <div className="flex min-h-11 items-center gap-2 text-xs">
            <Link className="shrink-0 py-3 font-semibold" to={`/discover?system=hongguo&id=${encodeURIComponent(row.source_id)}`}>E{String(row.episode).padStart(3, '0')}</Link>
            <span className="hidden min-w-0 flex-1 truncate text-ink-50 sm:block" title={row.relative_path}>{row.relative_path}</span>
            <span className={`shrink-0 rounded px-1.5 py-1 ${statusColors[row.status]}`}>{statusLabels[row.status]}</span>
            {row.status === 'downloading' && <span className="shrink-0 tabular-nums text-ink-50">{row.total_bytes > 0 ? `${Math.min(100, Math.floor(row.bytes * 100 / row.total_bytes))}%` : `${(row.bytes / 1048576).toFixed(1)} MB`}</span>}
            <details className="relative ml-auto shrink-0"><summary className="cursor-pointer px-2 py-3 text-ink-50" aria-label={`查看第 ${row.episode} 集任务详情`}>详情</summary><div className="absolute right-0 top-full z-10 max-h-80 w-64 max-w-[70vw] space-y-2 overflow-y-auto rounded-lg border border-ink-100/10 bg-[var(--app-bg)] p-3 shadow-xl">
              <p className="break-all">{row.relative_path}</p>
              <p>{row.status === 'completed' ? '下载来源' : '最近尝试来源'}：{sourceLabels[row.source ?? ''] || '未记录'}</p>
              <p>画质：{row.quality ? `${row.quality}p` : '未记录'} · 编码：{row.codec || '未记录'}</p>
              <p>分辨率：{row.width && row.height ? `${row.width} × ${row.height}` : '未记录'}{row.status !== 'completed' && row.width && row.height ? '（待校验）' : ''}</p>
              <p>已下载 {(row.bytes / 1048576).toFixed(1)} MB{row.total_bytes > 0 ? ` / ${(row.total_bytes / 1048576).toFixed(1)} MB` : ''}</p>
              {row.error && <p className="break-words text-red-500">{row.error}</p>}
              {Object.entries(row.source_errors ?? {}).map(([source, error]) => <p key={source} className="break-words text-ink-50">{sourceLabels[source] || '来源'}最近失败：{error}</p>)}
            </div></details>
            {['failed', 'cancelled'].includes(row.status) ? <button className="shrink-0 px-2 py-3 font-semibold text-brand-500 disabled:opacity-50" disabled={!!busy} onClick={() => void perform(row.id, 'retry')}>重试</button> : row.status !== 'completed' && <button className="shrink-0 px-2 py-3 text-ink-50 disabled:opacity-50" aria-label={`取消第 ${row.episode} 集下载`} disabled={!!busy} onClick={() => void perform(row.id, 'cancel')}>取消下载</button>}
          </div>
          {row.status === 'downloading' && <DownloadProgress label="分集下载进度" value={row.bytes} total={row.total_bytes} />}
        </div>)}
        {result.total > 50 && <div className="flex flex-wrap items-center gap-3 pt-3"><button className="btn-outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页分集</button><span>分集第 {page} 页 · 共 {result.total} 集</span><button className="btn-outline" disabled={page * 50 >= result.total} onClick={() => setPage(page + 1)}>下一页分集</button></div>}
      </>)}
    </div>
  </article>
}

function DownloadProgress({ label, value, total }: { label: string; value: number; total: number }) {
  const percent = total > 0 ? Math.max(0, Math.min(100, value * 100 / total)) : undefined
  return <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} aria-valuetext={percent === undefined ? '总大小未知' : undefined} className="h-0.5 overflow-hidden rounded-full bg-brand-500/10"><div className={`h-full rounded-full bg-gradient-to-r from-brand-500 to-sage-400 ${percent === undefined ? 'w-1/3 motion-safe:animate-pulse' : ''}`} style={percent === undefined ? undefined : { width: `${percent}%` }} /></div>
}
