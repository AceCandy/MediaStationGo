import { useEffect, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { Activity, ChevronLeft, ChevronRight, FileText, Play, RefreshCw, Search, Settings, X } from 'lucide-react'

import { libraryAPI, mediaAPI, type MediaScrapeIssue } from '../api/library'
import { tasksAPI, type BackgroundTask, type TaskDefinition, type TaskLog } from '../api/tasks'
import { confirmAction } from '../components/confirmAction'
import { ManualScrapeDialog } from '../components/ManualScrapeDialog'
import { ModalShell } from '../components/ModalShell'
import type { Library, Media } from '../types'
import { isSeriesLibraryType } from './librariesPageModel'

const scrapeLibraryTypes = new Set(['movie', 'tv', 'anime', 'variety', 'show', 'shows', 'nfo_movie', 'nfo_tv'])

function scrapeableLibraries(libraries: Library[]) {
  return libraries.filter((library) => scrapeLibraryTypes.has(library.type))
}

function hasTaskIssues(task?: BackgroundTask): boolean {
  return Boolean(task?.metrics?.errors || task?.metrics?.scan_errors || task?.metrics?.scrape_errors || task?.metrics?.failed)
}

function LatestResult({ task }: { task?: BackgroundTask }) {
	if (!task) return <span className="text-ink-50">尚未执行</span>
  if (task.status === 'failed') return <span className="text-red-500">失败</span>
  if (task.status === 'interrupted') return <span className="text-sand-500">中断</span>
  if (hasTaskIssues(task)) return <span className="text-orange-600">有异常</span>
	if (task.status === 'completed') return <span className="text-emerald-700">完成</span>
  return <span className="text-yellow-700">运行中</span>
}

function CurrentState({ state }: { state: TaskDefinition['current_state'] }) {
  return state === 'running'
    ? <span className="inline-flex rounded border border-yellow-300 px-1.5 py-0.5 text-xs text-yellow-700">运行中</span>
    : <span className="inline-flex rounded border border-gray-300 px-1.5 py-0.5 text-xs text-gray-600">空闲</span>
}

function formatTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function formatInterval(seconds: number): string {
  if (seconds % 86_400 === 0) return `${seconds / 86_400} 天`
  if (seconds % 3_600 === 0) return `${seconds / 3_600} 小时`
  return `${seconds / 60} 分钟`
}

function scheduleText(definition: TaskDefinition): string {
  const config = definition.schedule_config
  if (!config) return definition.schedule ? `每 ${definition.schedule}` : ''
  return `${config.enabled ? '已启用' : '已关闭'} · 每 ${formatInterval(config.interval_seconds)}`
}

function reverseLogLines(content: string): string {
  const trailingNewline = content.endsWith('\n')
  const lines = content.split('\n')
  if (trailingNewline) lines.pop()
  return lines.reverse().join('\n') + (trailingNewline ? '\n' : '')
}

const taskLogBadges = {
  '🔺': ['结束', '▼', 'bg-violet-500'],
  '🔻': ['开始', '▲', 'bg-violet-500'],
  '➕': ['新增', '+', 'bg-emerald-500'],
  '🗑': ['删除', '−', 'bg-rose-500'],
  '🔄': ['更新', '↻', 'bg-blue-500'],
  '✅': ['成功', '✓', 'bg-emerald-500'],
  '❌': ['失败', '×', 'bg-red-500'],
  '⏭': ['跳过', '»', 'bg-slate-500'],
  '⚠': ['警告', '!', 'bg-amber-500'],
  'ℹ': ['信息', 'i', 'bg-cyan-500'],
} as const

const taskLogMarkerPattern = /(🔺|🔻|➕|🗑️?|🔄|✅|❌|⏭️?|⚠️?|ℹ️?)/u
const legacyTaskLogPattern = /^(\S+\s+)\[(INFO|DETAIL|ERROR)\]\s+(.*)$/u

function renderLogContent(content: string) {
  const lines = reverseLogLines(content).split('\n')
  return lines.map((line, index) => {
    const legacy = legacyTaskLogPattern.exec(line)
    const visibleLine = legacy ? legacy[1] + legacy[3] : line
    const match = taskLogMarkerPattern.exec(visibleLine)
    const marker = match?.[0].replace('\uFE0F', '') as keyof typeof taskLogBadges | undefined
    const fallback = legacy ? (legacy[2] === 'ERROR' ? '❌' : 'ℹ') : undefined
    const badgeIndex = match?.index ?? legacy?.[1].length
    const badgeKey = marker ?? fallback
    if (!badgeKey || badgeIndex === undefined) return <span key={index}>{visibleLine}{index < lines.length - 1 ? '\n' : ''}</span>
    const badge = taskLogBadges[badgeKey]
    return (
      <span key={index}>
        {visibleLine.slice(0, badgeIndex)}
        <span role="img" aria-label={badge[0]} className={`inline-flex h-4 w-4 items-center justify-center rounded align-[-0.15em] font-sans text-[11px] font-black leading-none text-white ${badge[2]}`}>{badge[1]}</span>
        {visibleLine.slice(badgeIndex + (match?.[0].length ?? 0))}
        {index < lines.length - 1 ? '\n' : ''}
      </span>
    )
  })
}

interface TaskRowProps {
  definition: TaskDefinition
  running: string
  libraries: Library[]
  scanLibraryID: string
  onScanLibraryChange: (value: string) => void
  probeLibraryID: string
  onProbeLibraryChange: (value: string) => void
  probeLimit: string
  onProbeLimitChange: (value: string) => void
  scrapeLibraryID: string
  onScrapeLibraryChange: (value: string) => void
  onRun: (definition: TaskDefinition) => void
  onLog: (definition: TaskDefinition) => void
  onSchedule: (definition: TaskDefinition) => void
}

function TaskActions({ definition, running, libraries, scanLibraryID, onScanLibraryChange, probeLibraryID, onProbeLibraryChange, probeLimit, onProbeLimitChange, scrapeLibraryID, onScrapeLibraryChange, onRun, onLog, onSchedule }: TaskRowProps) {
  const disabled = definition.current_state === 'running' || running === definition.key
  const runDisabled = disabled || (definition.key === 'library_scan' && !scanLibraryID)
  return (
    <div className="flex flex-wrap items-center justify-end gap-1">
      {definition.key === 'library_scan' && (
        <select
          value={scanLibraryID}
          onChange={(event) => onScanLibraryChange(event.target.value)}
          disabled={disabled}
          aria-label="媒体库扫描媒体库"
          title="选择要扫描的媒体库"
          className="h-8 w-32 rounded border border-gray-200 px-2 text-xs text-ink-600"
        >
          <option value="">选择媒体库</option>
          {libraries.map((library) => <option key={library.id} value={library.id}>{library.name}</option>)}
        </select>
      )}
      {definition.action === 'probe_backfill' && (
        <>
          <select
            value={probeLibraryID}
            onChange={(event) => onProbeLibraryChange(event.target.value)}
            disabled={disabled}
            aria-label="媒体轨道回填媒体库"
            title="选择回填范围"
            className="h-8 w-32 rounded border border-gray-200 px-2 text-xs text-ink-600"
          >
            <option value="">全部媒体库</option>
            {libraries.map((library) => <option key={library.id} value={library.id}>{library.name}</option>)}
          </select>
          <input
            type="number"
            min="1"
            step="1"
            value={probeLimit}
            onChange={(event) => onProbeLimitChange(event.target.value)}
            disabled={disabled}
            placeholder="全部"
            aria-label="媒体轨道回填数量限制"
            title="留空表示当前范围内全量回填"
            className="h-8 w-20 rounded border border-gray-200 px-2 text-xs text-ink-600"
          />
        </>
      )}
      {definition.action === 'media_scrape' && (
        <select
          value={scrapeLibraryID}
          onChange={(event) => onScrapeLibraryChange(event.target.value)}
          disabled={disabled}
          aria-label="媒体入库刮削范围"
          title="选择要处理的媒体库"
          className="h-8 w-32 rounded border border-gray-200 px-2 text-xs text-ink-600"
        >
          <option value="">全部媒体库</option>
          {scrapeableLibraries(libraries).map((library) => <option key={library.id} value={library.id}>{library.name}</option>)}
        </select>
      )}
      {definition.action && (
        <button type="button" className="rounded border border-gray-200 p-2 text-sand-500 hover:text-brand-500 disabled:cursor-not-allowed disabled:opacity-40" title={`立即执行${definition.name}`} aria-label={`立即执行${definition.name}`} disabled={runDisabled} onClick={() => onRun(definition)}>
          <Play size={16} />
        </button>
      )}
      {definition.schedule_config && (
        <button type="button" className="icon-btn" title={`设置${definition.name}周期`} aria-label={`设置${definition.name}周期`} onClick={() => onSchedule(definition)}>
          <Settings size={16} />
        </button>
      )}
      <button type="button" className="rounded border border-gray-200 p-2 text-sand-500 hover:text-brand-500" title={`查看${definition.name}日志`} aria-label={`查看${definition.name}日志`} onClick={() => onLog(definition)}>
        <FileText size={16} />
      </button>
    </div>
  )
}

function DefinitionTable(props: { definitions: TaskDefinition[]; running: string; libraries: Library[]; scanLibraryID: string; onScanLibraryChange: TaskRowProps['onScanLibraryChange']; probeLibraryID: string; onProbeLibraryChange: TaskRowProps['onProbeLibraryChange']; probeLimit: string; onProbeLimitChange: TaskRowProps['onProbeLimitChange']; scrapeLibraryID: string; onScrapeLibraryChange: TaskRowProps['onScrapeLibraryChange']; onRun: TaskRowProps['onRun']; onLog: TaskRowProps['onLog']; onSchedule: TaskRowProps['onSchedule'] }) {
  return (
    <>
		<div className="hidden overflow-x-auto lg:block">
        <table className="w-full min-w-[880px] text-left text-sm">
          <thead className="text-xs text-sand-500">
            <tr><th className="py-2">任务</th><th>触发方式</th><th>当前状态</th><th>最近结果</th><th>执行时间</th><th className="text-right">操作</th></tr>
          </thead>
          <tbody>
            {props.definitions.map((definition) => (
              <tr key={definition.key} className="border-t border-gray-200 align-top">
				<td className="max-w-xs py-3"><div className="font-medium text-ink-600">{definition.name}</div><div className="mt-0.5 text-xs text-ink-50">{definition.description}</div></td>
				<td className="py-3 text-ink-100"><div>{definition.trigger}</div>{scheduleText(definition) && <div className="mt-0.5 text-xs text-ink-50">{scheduleText(definition)}</div>}</td>
                <td className="py-3"><CurrentState state={definition.current_state} /></td>
                <td className="py-3"><LatestResult task={definition.latest} /></td>
                <td className="whitespace-nowrap py-3 text-ink-100"><div>最近 · {formatTime(definition.latest?.finished_at ?? definition.latest?.started_at)}</div><div className="mt-0.5 text-xs text-ink-50">下次 · {formatTime(definition.next_run)}</div></td>
                <td className="py-3"><TaskActions {...props} definition={definition} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
		<div className="divide-y divide-gray-200 lg:hidden">
        {props.definitions.map((definition) => (
          <section key={definition.key} className="py-4 first:pt-0 last:pb-0">
			<div className="flex items-start justify-between gap-3"><div><h2 className="font-medium text-ink-600">{definition.name}</h2><p className="mt-0.5 text-xs text-ink-50">{definition.description}</p></div><TaskActions {...props} definition={definition} /></div>
            <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
			<div><dt className="text-ink-50">触发方式</dt><dd className="mt-0.5 text-ink-100">{definition.trigger}{scheduleText(definition) ? ` · ${scheduleText(definition)}` : ''}</dd></div>
			<div><dt className="text-ink-50">当前状态</dt><dd className="mt-0.5"><CurrentState state={definition.current_state} /></dd></div>
			<div><dt className="text-ink-50">最近结果</dt><dd className="mt-0.5"><LatestResult task={definition.latest} /></dd></div>
			<div><dt className="text-ink-50">执行时间</dt><dd className="mt-0.5 text-ink-100"><div>最近 · {formatTime(definition.latest?.finished_at ?? definition.latest?.started_at)}</div>{definition.schedule_config && <div className="mt-0.5 text-ink-50">下次 · {formatTime(definition.next_run)}</div>}</dd></div>
            </dl>
          </section>
        ))}
      </div>
    </>
  )
}

const scheduleUnits = {
  minute: { label: '分钟', seconds: 60 },
  hour: { label: '小时', seconds: 3_600 },
  day: { label: '天', seconds: 86_400 },
} as const

type ScheduleUnit = keyof typeof scheduleUnits

function scheduleUnitFor(seconds: number): ScheduleUnit {
  if (seconds % scheduleUnits.day.seconds === 0) return 'day'
  if (seconds % scheduleUnits.hour.seconds === 0) return 'hour'
  return 'minute'
}

function TaskScheduleDialog({ definition, onClose, onSaved }: { definition: TaskDefinition; onClose: () => void; onSaved: () => Promise<void> }) {
  const config = definition.schedule_config!
  const initialUnit = scheduleUnitFor(config.interval_seconds)
  const [enabled, setEnabled] = useState(config.enabled)
  const [unit, setUnit] = useState<ScheduleUnit>(initialUnit)
  const [value, setValue] = useState(String(config.interval_seconds / scheduleUnits[initialUnit].seconds))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const unitSeconds = scheduleUnits[unit].seconds

  const save = async () => {
    setSaving(true)
    setError('')
    try {
      await tasksAPI.updateSchedule(definition.key, enabled, Number(value) * unitSeconds)
      await onSaved()
      toast.success(`${definition.name}周期已更新`)
      onClose()
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '周期设置保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <ModalShell onClose={saving ? undefined : onClose} maxWidth="max-w-md" className="p-5 sm:p-6" ariaLabel={`设置${definition.name}周期`}>
      <header className="flex items-start justify-between gap-3"><div><h2 className="font-display text-lg font-semibold text-ink-600">{definition.name}</h2><p className="mt-1 text-xs text-ink-50">设置后台定时执行开关与周期</p></div><button type="button" className="icon-btn" title="关闭" aria-label="关闭" disabled={saving} onClick={onClose}><X size={18} /></button></header>
      <label className="mt-5 flex items-center justify-between gap-4 text-sm text-ink-600"><span>启用定时执行</span><input type="checkbox" checked={enabled} disabled={saving} onChange={(event) => setEnabled(event.target.checked)} /></label>
      <div className="mt-4 grid grid-cols-[minmax(0,1fr)_7rem] gap-2">
        <label><span className="mb-1 block text-xs text-ink-50">执行周期</span><input className="input-field" type="number" min={Math.ceil(config.min_interval_seconds / unitSeconds)} max={Math.floor(config.max_interval_seconds / unitSeconds)} step="1" required value={value} disabled={saving} onChange={(event) => setValue(event.target.value)} /></label>
        <label><span className="mb-1 block text-xs text-ink-50">单位</span><select className="input-field" value={unit} disabled={saving} onChange={(event) => setUnit(event.target.value as ScheduleUnit)}>{Object.entries(scheduleUnits).map(([key, item]) => <option key={key} value={key}>{item.label}</option>)}</select></label>
      </div>
      <p className="mt-2 text-xs text-ink-50">支持范围：{formatInterval(config.min_interval_seconds)} 至 {formatInterval(config.max_interval_seconds)}</p>
      {error && <p className="mt-3 text-sm text-red-500" role="alert">{error}</p>}
      <footer className="mt-6 flex justify-end gap-2"><button type="button" className="btn-outline" disabled={saving} onClick={onClose}>取消</button><button type="button" className="btn-primary" disabled={saving || !value} onClick={() => void save()}>{saving ? '保存中...' : '保存'}</button></footer>
    </ModalShell>
  )
}

function TaskLogDialog({ definition, onClose }: { definition: TaskDefinition; onClose: () => void }) {
  const [log, setLog] = useState<TaskLog | null>(null)
  const [month, setMonth] = useState(() => new Date(new Date().getFullYear(), new Date().getMonth(), 1))
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const requestID = useRef(0)

  useEffect(() => {
    let active = true
    const currentRequest = ++requestID.current
    setError('')
    setLoading(true)
    setLog(null)
    tasksAPI.log(definition.key)
      .then((value) => {
        if (!active || requestID.current !== currentRequest) return
        setLog(value)
        if (value.date) setMonth(monthFromDateKey(value.date))
      })
      .catch(() => { if (active && requestID.current === currentRequest) setError('任务日志读取失败') })
      .finally(() => { if (active && requestID.current === currentRequest) setLoading(false) })
    return () => { active = false }
  }, [definition.key])

  const loadLog = (date?: string) => {
    const currentRequest = ++requestID.current
    setError('')
    setLoading(true)
    tasksAPI.log(definition.key, date)
      .then((value) => { if (requestID.current === currentRequest) setLog(value) })
      .catch(() => { if (requestID.current === currentRequest) setError('任务日志读取失败') })
      .finally(() => { if (requestID.current === currentRequest) setLoading(false) })
  }

  return (
    <ModalShell onClose={onClose} maxWidth="max-w-5xl" className="flex max-h-[90vh] flex-col gap-3 p-4 sm:p-6" ariaLabel={`${definition.name}日志`}>
      <header className="flex items-start justify-between gap-3"><div><h2 className="font-display text-lg font-semibold text-ink-600">{definition.name}</h2><p className="text-xs text-ink-50">按日期查看详细日志</p></div><button type="button" className="icon-btn" title="关闭" aria-label="关闭" onClick={onClose}><X size={18} /></button></header>
      <div className="grid min-h-0 flex-1 gap-4 md:grid-cols-[17rem_minmax(0,1fr)]">
        <TaskLogCalendar month={month} dates={log?.dates ?? []} selected={log?.date ?? ''} onMonthChange={setMonth} onSelect={loadLog} />
        <div className="flex min-h-64 min-w-0 flex-col">
          <div className="mb-2 flex h-9 items-center justify-between text-xs text-sand-500">
            <span>{log?.date ? formatDateKey(log.date) : ''}</span>
            <button type="button" className="icon-btn" title="刷新日志" aria-label="刷新日志" disabled={loading} onClick={() => loadLog(log?.date)}><RefreshCw size={16} className={loading ? 'animate-spin' : ''} /></button>
          </div>
          <pre className="min-h-56 flex-1 overflow-auto whitespace-pre-wrap break-words rounded border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-relaxed text-gray-100">{loading ? '加载日志中...' : error ? error : log?.content ? renderLogContent(log.content) : '该任务暂无日志。'}</pre>
          {log?.truncated && <p className="mt-1 text-xs text-orange-600">日志过长，当前显示末尾内容。</p>}
        </div>
      </div>
    </ModalShell>
  )
}

const weekDays = ['日', '一', '二', '三', '四', '五', '六']

function TaskLogCalendar({ month, dates, selected, onMonthChange, onSelect }: { month: Date; dates: string[]; selected: string; onMonthChange: (month: Date) => void; onSelect: (date: string) => void }) {
  const available = new Set(dates)
  const firstDay = new Date(month.getFullYear(), month.getMonth(), 1).getDay()
  const daysInMonth = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const cells = Array.from({ length: 42 }, (_, index) => {
    const day = index - firstDay + 1
    return day > 0 && day <= daysInMonth ? day : null
  })
  const moveMonth = (offset: number) => onMonthChange(new Date(month.getFullYear(), month.getMonth() + offset, 1))

  return (
    <section className="border-b border-gray-200 pb-4 md:border-b-0 md:border-r md:pb-0 md:pr-4" aria-label="日志日期">
      <div className="mb-3 flex h-9 items-center justify-between">
        <button type="button" className="icon-btn" title="上个月" aria-label="上个月" onClick={() => moveMonth(-1)}><ChevronLeft size={17} /></button>
        <h3 className="text-sm font-medium text-ink-600">{month.getFullYear()} 年 {month.getMonth() + 1} 月</h3>
        <button type="button" className="icon-btn" title="下个月" aria-label="下个月" onClick={() => moveMonth(1)}><ChevronRight size={17} /></button>
      </div>
      <div className="grid grid-cols-7 gap-1 text-center">
        {weekDays.map((day) => <span key={day} className="flex h-8 items-center justify-center text-xs text-ink-50">{day}</span>)}
        {cells.map((day, index) => {
          if (!day) return <span key={`empty-${index}`} className="h-8" aria-hidden="true" />
          const date = dateKey(month.getFullYear(), month.getMonth() + 1, day)
          const enabled = available.has(date)
          return <button type="button" key={date} className={`h-8 border text-xs ${selected === date ? 'border-brand-500 bg-brand-500 text-white' : enabled ? 'border-gray-200 text-ink-600 hover:border-brand-400 hover:text-brand-500' : 'border-transparent text-ink-50 opacity-35'}`} disabled={!enabled} aria-pressed={selected === date} onClick={() => onSelect(date)}>{day}</button>
        })}
      </div>
    </section>
  )
}

function dateKey(year: number, month: number, day: number): string {
  return `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
}

function monthFromDateKey(value: string): Date {
  const [year, month] = value.split('-').map(Number)
  return new Date(year, month - 1, 1)
}

function formatDateKey(value: string): string {
  const [year, month, day] = value.split('-').map(Number)
  return new Date(year, month - 1, day).toLocaleDateString()
}

function ScrapeIssuesPanel({ libraries }: { libraries: Library[] }) {
  const [items, setItems] = useState<MediaScrapeIssue[]>([])
  const [total, setTotal] = useState(0)
  const [libraryID, setLibraryID] = useState('')
  const [status, setStatus] = useState<'' | 'error' | 'no_match'>('')
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [retrying, setRetrying] = useState('')
  const [openingManual, setOpeningManual] = useState('')
  const [manualTarget, setManualTarget] = useState<{ media: Media; mediaType: string } | null>(null)
  const pageSize = 20

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError(false)
    mediaAPI.listScrapeIssues({ libraryID, status: status || undefined, page, pageSize })
      .then((value) => {
        if (!active) return
        setItems(value.items ?? [])
        setTotal(value.total ?? 0)
      })
      .catch(() => {
        if (active) setLoadError(true)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => { active = false }
  }, [libraryID, page, refreshVersion, status])

  const refreshIssues = () => setRefreshVersion((value) => value + 1)
  const retry = async (issue: MediaScrapeIssue) => {
    setRetrying(issue.id)
    try {
      await mediaAPI.retryScrape(issue.id)
      toast.success('已加入媒体入库刮削队列')
      refreshIssues()
    } catch {
      toast.error('重新刮削触发失败')
    } finally {
      setRetrying('')
    }
  }
  const openManualMatch = async (issue: MediaScrapeIssue) => {
    setOpeningManual(issue.id)
    try {
      const media = await mediaAPI.get(issue.id)
      const mediaType = isSeriesLibraryType(issue.library_type) ? 'tv' : 'movie'
      setManualTarget({ media, mediaType })
    } catch {
      toast.error('媒体信息加载失败')
    } finally {
      setOpeningManual('')
    }
  }
  const pageCount = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="mt-6 border-t border-gray-200 pt-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-display text-lg font-semibold text-ink-600">媒体入库刮削待处理</h2>
          <p className="mt-1 text-xs text-ink-50">统一显示刮削失败和未匹配记录；成功来源请在上方任务日志中查看。</p>
        </div>
        <button type="button" className="icon-btn" title="刷新待处理记录" aria-label="刷新待处理记录" disabled={loading} onClick={refreshIssues}>
          <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-2">
        <label>
          <span className="mb-1 block text-xs text-ink-50">媒体库</span>
          <select className="input-field" value={libraryID} onChange={(event) => { setLibraryID(event.target.value); setPage(1) }}>
            <option value="">全部媒体库</option>
            {scrapeableLibraries(libraries).map((library) => <option key={library.id} value={library.id}>{library.name}</option>)}
          </select>
        </label>
        <label>
          <span className="mb-1 block text-xs text-ink-50">状态</span>
          <select className="input-field" value={status} onChange={(event) => { setStatus(event.target.value as typeof status); setPage(1) }}>
            <option value="">全部待处理</option>
            <option value="error">刮削失败</option>
            <option value="no_match">未匹配</option>
          </select>
        </label>
      </div>
      <div className="mt-4 space-y-2">
        {loading && items.length === 0 ? <p className="py-6 text-center text-sm text-ink-50">加载中...</p>
          : loadError ? <p className="py-6 text-center text-sm text-red-500">待处理记录加载失败。</p>
            : items.length === 0 ? <p className="py-6 text-center text-sm text-ink-50">当前没有刮削失败或未匹配记录。</p>
              : items.map((issue) => {
                const nfoOnly = issue.library_type === 'nfo_movie' || issue.library_type === 'nfo_tv'
                const episode = issue.episode_num > 0 ? ` · S${String(issue.season_num).padStart(2, '0')}E${String(issue.episode_num).padStart(2, '0')}` : ''
                return (
                  <article key={issue.id} className="flex min-w-0 flex-col gap-3 rounded border border-gray-200 p-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="break-words text-sm font-medium text-ink-600">{issue.title || issue.id}{issue.year > 0 ? ` (${issue.year})` : ''}{episode}</h3>
                        <span className={issue.scrape_status === 'error' ? 'text-xs text-red-500' : 'text-xs text-orange-600'}>{issue.scrape_status === 'error' ? '刮削失败' : '未匹配'}</span>
                      </div>
                      <p className="mt-1 break-words text-xs text-ink-50">{issue.library_name} · {issue.reason}</p>
                      <p className="mt-1 break-words text-xs text-ink-50">原始路径：{issue.path}</p>
                      {nfoOnly && <p className="mt-1 text-xs text-ink-50">请补充或修复本地 NFO 后重试。</p>}
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-2">
                      {issue.scrape_status === 'no_match' && !nfoOnly && (
                        <button type="button" className="btn-outline px-3 py-2 text-xs" disabled={Boolean(openingManual)} onClick={() => void openManualMatch(issue)}>
                          <Search size={14} /> {openingManual === issue.id ? '加载中...' : '手动匹配'}
                        </button>
                      )}
                      <button type="button" className="btn-outline px-3 py-2 text-xs" disabled={Boolean(retrying)} onClick={() => void retry(issue)}>
                        <RefreshCw size={14} className={retrying === issue.id ? 'animate-spin' : ''} /> {retrying === issue.id ? '排队中...' : '重试'}
                      </button>
                    </div>
                  </article>
                )
              })}
      </div>
      {total > pageSize && (
        <div className="mt-4 flex items-center justify-between text-xs text-ink-50">
          <span>共 {total} 条</span>
          <div className="flex items-center gap-2">
            <button type="button" className="icon-btn" title="上一页" aria-label="上一页" disabled={page <= 1 || loading} onClick={() => setPage((value) => value - 1)}><ChevronLeft size={16} /></button>
            <span>{page} / {pageCount}</span>
            <button type="button" className="icon-btn" title="下一页" aria-label="下一页" disabled={page >= pageCount || loading} onClick={() => setPage((value) => value + 1)}><ChevronRight size={16} /></button>
          </div>
        </div>
      )}
      <ManualScrapeDialog
        open={Boolean(manualTarget)}
        media={manualTarget?.media ?? null}
        defaultQuery={manualTarget?.media.title}
        mediaType={manualTarget?.mediaType}
        onClose={() => setManualTarget(null)}
        onApplied={() => { setManualTarget(null); refreshIssues() }}
      />
    </div>
  )
}

export function TasksPage() {
	const [definitions, setDefinitions] = useState<TaskDefinition[] | null>(null)
	const [loadError, setLoadError] = useState(false)
  const [logDefinition, setLogDefinition] = useState<TaskDefinition | null>(null)
	const [scheduleDefinition, setScheduleDefinition] = useState<TaskDefinition | null>(null)
	const [running, setRunning] = useState('')
	const [libraries, setLibraries] = useState<Library[]>([])
	const [scanLibraryID, setScanLibraryID] = useState('')
	const [probeLibraryID, setProbeLibraryID] = useState('')
	const [probeLimit, setProbeLimit] = useState('')
	const [scrapeLibraryID, setScrapeLibraryID] = useState('')
	const runPending = useRef(false)

	const refresh = () => tasksAPI.snapshot(1, 1).then((value) => { setDefinitions(value.definitions ?? []); setLoadError(false) })
  useEffect(() => {
    let active = true
		const tick = () => tasksAPI.snapshot(1, 1).then((value) => { if (active) { setDefinitions(value.definitions ?? []); setLoadError(false) } }).catch(() => { if (active) setLoadError(true) })
    void tick()
    const id = window.setInterval(tick, 3_000)
    return () => { active = false; window.clearInterval(id) }
  }, [])
  useEffect(() => {
    libraryAPI.list({ includeHidden: true }).then(setLibraries).catch(() => setLibraries([]))
  }, [])

  const run = async (definition: TaskDefinition) => {
		if (runPending.current || running || definition.current_state === 'running') return
		runPending.current = true
    setRunning(definition.key)
    try {
			if (definition.key === 'account_cleanup' && !await confirmAction({
				title: '确认执行账号清理巡检',
				message: '将立即按当前保号规则检查并清理不符合条件的账号，请确认是否继续。',
				confirmText: '确认执行',
				danger: true,
			})) return
      const limit = definition.action === 'probe_backfill' && probeLimit ? Number(probeLimit) : undefined
      if (limit !== undefined && (!Number.isInteger(limit) || limit < 1)) {
        toast.error('回填数量必须是正整数')
        return
      }
			await tasksAPI.run(definition.key, definition.key === 'library_scan'
        ? { library_id: scanLibraryID }
        : definition.action === 'probe_backfill' ? { limit, library_id: probeLibraryID || undefined }
          : definition.action === 'media_scrape' ? (scrapeLibraryID ? { library_id: scrapeLibraryID } : { all_libraries: true }) : undefined)
			toast.success(`${definition.name}已触发`)
			await refresh().catch(() => setLoadError(true))
    } catch (err: unknown) {
      const message = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '任务触发失败'
      toast.error(message)
    } finally {
			runPending.current = false
      setRunning('')
    }
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3"><Activity className="h-6 w-6 text-brand-500" /><div><h1 className="font-display text-3xl font-bold text-ink-600">任务中心</h1><p className="text-sm text-ink-50">查看后台任务状态、调度与最近执行结果。</p></div></header>
		<section className="glass-panel">
			{loadError && !definitions ? <div className="flex flex-col items-center gap-3 py-8 text-sm text-ink-50"><p>任务列表加载失败。</p><button type="button" className="rounded border border-gray-200 p-2 text-sand-600 hover:text-brand-500" title="重新加载" aria-label="重新加载" onClick={() => void refresh()}><RefreshCw size={16} /></button></div> : !definitions ? <p className="py-8 text-center text-ink-50">加载中...</p> : definitions.length === 0 ? <p className="py-8 text-center text-ink-50">暂无任务。</p> : <><DefinitionTable definitions={definitions} running={running} libraries={libraries} scanLibraryID={scanLibraryID} onScanLibraryChange={setScanLibraryID} probeLibraryID={probeLibraryID} onProbeLibraryChange={setProbeLibraryID} probeLimit={probeLimit} onProbeLimitChange={setProbeLimit} scrapeLibraryID={scrapeLibraryID} onScrapeLibraryChange={setScrapeLibraryID} onRun={(definition) => void run(definition)} onLog={setLogDefinition} onSchedule={setScheduleDefinition} /><ScrapeIssuesPanel libraries={libraries} /></>}
      </section>
      {logDefinition && <TaskLogDialog definition={logDefinition} onClose={() => setLogDefinition(null)} />}
      {scheduleDefinition && <TaskScheduleDialog definition={scheduleDefinition} onClose={() => setScheduleDefinition(null)} onSaved={() => refresh().catch(() => setLoadError(true))} />}
    </div>
  )
}
