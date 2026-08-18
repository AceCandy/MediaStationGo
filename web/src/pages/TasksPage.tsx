import { useEffect, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { Activity, ChevronLeft, ChevronRight, FileText, Play, RefreshCw, X } from 'lucide-react'

import { libraryAPI } from '../api/library'
import { tasksAPI, type BackgroundTask, type TaskDefinition, type TaskLog } from '../api/tasks'
import { ModalShell } from '../components/ModalShell'
import type { Library } from '../types'

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

function reverseLogLines(content: string): string {
  const trailingNewline = content.endsWith('\n')
  const lines = content.split('\n')
  if (trailingNewline) lines.pop()
  return lines.reverse().join('\n') + (trailingNewline ? '\n' : '')
}

interface TaskRowProps {
  definition: TaskDefinition
  running: string
  libraries: Library[]
  probeLibraryID: string
  onProbeLibraryChange: (value: string) => void
  probeLimit: string
  onProbeLimitChange: (value: string) => void
  onRun: (definition: TaskDefinition) => void
  onLog: (definition: TaskDefinition) => void
}

function TaskActions({ definition, running, libraries, probeLibraryID, onProbeLibraryChange, probeLimit, onProbeLimitChange, onRun, onLog }: TaskRowProps) {
  const disabled = definition.current_state === 'running' || running === definition.key
  return (
    <div className="flex flex-wrap items-center justify-end gap-1">
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
      {definition.action && (
        <button type="button" className="rounded border border-gray-200 p-2 text-sand-500 hover:text-brand-500 disabled:cursor-not-allowed disabled:opacity-40" title={`立即执行${definition.name}`} aria-label={`立即执行${definition.name}`} disabled={disabled} onClick={() => onRun(definition)}>
          <Play size={16} />
        </button>
      )}
      <button type="button" className="rounded border border-gray-200 p-2 text-sand-500 hover:text-brand-500" title={`查看${definition.name}日志`} aria-label={`查看${definition.name}日志`} onClick={() => onLog(definition)}>
        <FileText size={16} />
      </button>
    </div>
  )
}

function DefinitionTable(props: { definitions: TaskDefinition[]; running: string; libraries: Library[]; probeLibraryID: string; onProbeLibraryChange: TaskRowProps['onProbeLibraryChange']; probeLimit: string; onProbeLimitChange: TaskRowProps['onProbeLimitChange']; onRun: TaskRowProps['onRun']; onLog: TaskRowProps['onLog'] }) {
  return (
    <>
		<div className="hidden overflow-x-auto lg:block">
        <table className="w-full min-w-[980px] text-left text-sm">
          <thead className="text-xs text-sand-500">
            <tr><th className="py-2">任务</th><th>触发方式</th><th>当前状态</th><th>最近结果</th><th>最近执行</th><th>下次执行</th><th className="text-right">操作</th></tr>
          </thead>
          <tbody>
            {props.definitions.map((definition) => (
              <tr key={definition.key} className="border-t border-gray-200 align-top">
				<td className="max-w-xs py-3"><div className="font-medium text-ink-600">{definition.name}</div><div className="mt-0.5 text-xs text-ink-50">{definition.description}</div></td>
				<td className="py-3 text-ink-100"><div>{definition.trigger}</div>{definition.schedule && <div className="mt-0.5 text-xs text-ink-50">每 {definition.schedule}</div>}</td>
                <td className="py-3"><CurrentState state={definition.current_state} /></td>
                <td className="py-3"><LatestResult task={definition.latest} /></td>
                <td className="whitespace-nowrap py-3 text-ink-100">{formatTime(definition.latest?.finished_at ?? definition.latest?.started_at)}</td>
                <td className="whitespace-nowrap py-3 text-ink-100">{formatTime(definition.next_run)}</td>
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
			<div><dt className="text-ink-50">触发方式</dt><dd className="mt-0.5 text-ink-100">{definition.trigger}{definition.schedule ? ` · 每 ${definition.schedule}` : ''}</dd></div>
			<div><dt className="text-ink-50">当前状态</dt><dd className="mt-0.5"><CurrentState state={definition.current_state} /></dd></div>
			<div><dt className="text-ink-50">最近结果</dt><dd className="mt-0.5"><LatestResult task={definition.latest} /></dd></div>
			<div><dt className="text-ink-50">最近执行</dt><dd className="mt-0.5 text-ink-100">{formatTime(definition.latest?.finished_at ?? definition.latest?.started_at)}</dd></div>
			{definition.schedule && <div className="col-span-2"><dt className="text-ink-50">下次执行</dt><dd className="mt-0.5 text-ink-100">{formatTime(definition.next_run)}</dd></div>}
            </dl>
          </section>
        ))}
      </div>
    </>
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

  const selectDate = (date: string) => {
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
        <TaskLogCalendar month={month} dates={log?.dates ?? []} selected={log?.date ?? ''} onMonthChange={setMonth} onSelect={selectDate} />
        <div className="flex min-h-64 min-w-0 flex-col">
          <div className="mb-2 h-5 text-xs text-sand-500">{log?.date ? formatDateKey(log.date) : ''}</div>
          <pre className="min-h-56 flex-1 overflow-auto whitespace-pre-wrap break-words rounded border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-relaxed text-gray-100">{loading ? '加载日志中...' : error ? error : log?.content ? reverseLogLines(log.content) : '该任务暂无日志。'}</pre>
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

export function TasksPage() {
	const [definitions, setDefinitions] = useState<TaskDefinition[] | null>(null)
	const [loadError, setLoadError] = useState(false)
  const [logDefinition, setLogDefinition] = useState<TaskDefinition | null>(null)
	const [running, setRunning] = useState('')
	const [libraries, setLibraries] = useState<Library[]>([])
	const [probeLibraryID, setProbeLibraryID] = useState('')
	const [probeLimit, setProbeLimit] = useState('')

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
    if (running || definition.current_state === 'running') return
    setRunning(definition.key)
    try {
      const limit = definition.action === 'probe_backfill' && probeLimit ? Number(probeLimit) : undefined
      if (limit !== undefined && (!Number.isInteger(limit) || limit < 1)) {
        toast.error('回填数量必须是正整数')
        return
      }
			await tasksAPI.run(definition.key, definition.action === 'probe_backfill' ? {
        limit,
        library_id: probeLibraryID || undefined,
      } : undefined)
			toast.success(`${definition.name}已触发`)
			await refresh().catch(() => setLoadError(true))
    } catch (err: unknown) {
      const message = (err as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '任务触发失败'
      toast.error(message)
    } finally {
      setRunning('')
    }
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center gap-3"><Activity className="h-6 w-6 text-brand-500" /><div><h1 className="font-display text-3xl font-bold text-ink-600">任务中心</h1><p className="text-sm text-ink-50">查看后台任务状态、调度与最近执行结果。</p></div></header>
		<section className="glass-panel">
			{loadError && !definitions ? <div className="flex flex-col items-center gap-3 py-8 text-sm text-ink-50"><p>任务列表加载失败。</p><button type="button" className="rounded border border-gray-200 p-2 text-sand-600 hover:text-brand-500" title="重新加载" aria-label="重新加载" onClick={() => void refresh()}><RefreshCw size={16} /></button></div> : !definitions ? <p className="py-8 text-center text-ink-50">加载中...</p> : definitions.length === 0 ? <p className="py-8 text-center text-ink-50">暂无任务。</p> : <DefinitionTable definitions={definitions} running={running} libraries={libraries} probeLibraryID={probeLibraryID} onProbeLibraryChange={setProbeLibraryID} probeLimit={probeLimit} onProbeLimitChange={setProbeLimit} onRun={(definition) => void run(definition)} onLog={setLogDefinition} />}
      </section>
      {logDefinition && <TaskLogDialog definition={logDefinition} onClose={() => setLogDefinition(null)} />}
    </div>
  )
}
