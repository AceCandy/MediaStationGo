import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Activity, ChevronLeft, ChevronRight, FileText, Play, RefreshCw, X } from 'lucide-react'

import { tasksAPI, type BackgroundTask, type TaskDefinition, type TaskHistory, type TaskLog } from '../api/tasks'

const HISTORY_PAGE_SIZE = 20

const metricLabels: Record<string, string> = {
  organized: '新增', replaced: '替换', reclassified: '纠偏', skipped: '跳过', total: '总数',
  completed: '完成', failed: '失败', errors: '错误', libraries: '媒体库', visited: '访问',
  added: '入库', updated: '更新', removed: '移除', matched: '匹配', processed: '处理', deleted: '删除',
  scan_visited: '访问', scan_added: '入库', scan_updated: '更新', scan_removed: '移除', scan_errors: '扫描错误',
  scrape_matched: '匹配', scrape_processed: '刮削处理', scrape_errors: '刮削错误',
}

const triggerLabels: Record<BackgroundTask['trigger'], string> = {
  manual: '手动', scheduled: '定时', event: '事件',
}

function formatMetrics(metrics?: Record<string, number>): string {
  if (!metrics) return ''
  return Object.entries(metrics)
    .filter(([, value]) => Number.isFinite(value) && value !== 0)
    .map(([key, value]) => `${metricLabels[key] ?? key} ${value}`)
    .join(' · ')
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

interface TaskRowProps {
  definition: TaskDefinition
  running: string
  onRun: (definition: TaskDefinition) => void
  onLog: (definition: TaskDefinition) => void
}

function TaskActions({ definition, running, onRun, onLog }: TaskRowProps) {
  const disabled = definition.current_state === 'running' || running === definition.key
  return (
    <div className="flex items-center justify-end gap-1">
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

function DefinitionTable(props: { definitions: TaskDefinition[]; running: string; onRun: TaskRowProps['onRun']; onLog: TaskRowProps['onLog'] }) {
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
  const [history, setHistory] = useState<TaskHistory | null>(null)
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<BackgroundTask | null>(null)
  const [log, setLog] = useState<TaskLog | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    setError('')
    tasksAPI.history(definition.key, page, HISTORY_PAGE_SIZE)
      .then((value) => {
        if (!active) return
        setHistory(value)
        setSelected((current) => current && value.items.some((item) => item.id === current.id) ? current : (value.items[0] ?? null))
      })
      .catch(() => active && setError('任务执行记录读取失败'))
    return () => { active = false }
  }, [definition.key, page])

  useEffect(() => {
    let active = true
    setError('')
    setLog(null)
    if (!selected) return () => { active = false }
    tasksAPI.log(selected.id).then((value) => active && setLog(value)).catch(() => active && setError('任务日志读取失败'))
    return () => { active = false }
  }, [selected])

  const pages = Math.max(1, Math.ceil((history?.total ?? 0) / HISTORY_PAGE_SIZE))
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-3 backdrop-blur-sm" onClick={onClose}>
      <section role="dialog" aria-modal="true" aria-label={`${definition.name}日志`} className="glass-panel flex max-h-[90vh] w-full max-w-5xl flex-col gap-3" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-start justify-between gap-3"><div><h2 className="font-display text-lg font-semibold text-ink-600">{definition.name}</h2><p className="text-xs text-ink-50">执行记录与详细日志</p></div><button type="button" className="rounded p-2 text-sand-500 hover:bg-gray-100" title="关闭" aria-label="关闭" onClick={onClose}><X size={18} /></button></header>
        {error ? <p className="text-sm text-red-500">{error}</p> : !history ? <p className="py-8 text-center text-sm text-sand-500">加载中...</p> : history.items.length === 0 ? <p className="py-8 text-center text-sm text-sand-500">该任务尚无执行记录。</p> : (
          <div className="grid min-h-0 flex-1 gap-3 md:grid-cols-[18rem_minmax(0,1fr)]">
            <div className="flex min-h-0 flex-col border-b border-gray-200 pb-3 md:border-b-0 md:border-r md:pb-0 md:pr-3">
              <div className="max-h-44 overflow-auto md:max-h-none md:flex-1">
                {history.items.map((task) => <button type="button" key={task.id} className={`block w-full border-b border-gray-200 px-2 py-2 text-left text-xs last:border-0 ${selected?.id === task.id ? 'bg-gray-100' : 'hover:bg-gray-50'}`} onClick={() => setSelected(task)}><span className="font-medium text-ink-600">{formatTime(task.started_at)}</span><span className="ml-2 text-sand-500">{triggerLabels[task.trigger]}</span><div className="mt-1 truncate text-sand-500">{task.error || task.message || task.status}</div></button>)}
              </div>
              <nav className="mt-2 flex items-center justify-between text-xs text-ink-100" aria-label="执行记录分页"><button type="button" className="rounded border border-gray-200 p-1.5 disabled:opacity-40" title="上一页" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}><ChevronLeft size={15} /></button><span>{page} / {pages}</span><button type="button" className="rounded border border-gray-200 p-1.5 disabled:opacity-40" title="下一页" disabled={page >= pages} onClick={() => setPage((value) => value + 1)}><ChevronRight size={15} /></button></nav>
            </div>
            <div className="flex min-h-48 min-w-0 flex-col"><div className="mb-2 text-xs text-sand-500">{selected && <>{selected.error || selected.message || selected.status}{formatMetrics(selected.metrics) && <span className="ml-2">{formatMetrics(selected.metrics)}</span>}</>}</div><pre className="min-h-40 flex-1 overflow-auto whitespace-pre-wrap break-words rounded border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-relaxed text-gray-100">{log ? (log.content || '暂无详细日志。') : '加载日志中...'}</pre>{log?.truncated && <p className="mt-1 text-xs text-orange-600">日志过长，当前显示末尾内容。</p>}</div>
          </div>
        )}
      </section>
    </div>
  )
}

export function TasksPage() {
	const [definitions, setDefinitions] = useState<TaskDefinition[] | null>(null)
	const [loadError, setLoadError] = useState(false)
  const [logDefinition, setLogDefinition] = useState<TaskDefinition | null>(null)
  const [running, setRunning] = useState('')

	const refresh = () => tasksAPI.snapshot(1, 1).then((value) => { setDefinitions(value.definitions ?? []); setLoadError(false) })
  useEffect(() => {
    let active = true
		const tick = () => tasksAPI.snapshot(1, 1).then((value) => { if (active) { setDefinitions(value.definitions ?? []); setLoadError(false) } }).catch(() => { if (active) setLoadError(true) })
    void tick()
    const id = window.setInterval(tick, 3_000)
    return () => { active = false; window.clearInterval(id) }
  }, [])

  const run = async (definition: TaskDefinition) => {
    if (running || definition.current_state === 'running') return
    setRunning(definition.key)
    try {
			await tasksAPI.run(definition.key)
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
			{loadError && !definitions ? <div className="flex flex-col items-center gap-3 py-8 text-sm text-ink-50"><p>任务列表加载失败。</p><button type="button" className="rounded border border-gray-200 p-2 text-sand-600 hover:text-brand-500" title="重新加载" aria-label="重新加载" onClick={() => void refresh()}><RefreshCw size={16} /></button></div> : !definitions ? <p className="py-8 text-center text-ink-50">加载中...</p> : definitions.length === 0 ? <p className="py-8 text-center text-ink-50">暂无任务。</p> : <DefinitionTable definitions={definitions} running={running} onRun={(definition) => void run(definition)} onLog={setLogDefinition} />}
      </section>
      {logDefinition && <TaskLogDialog definition={logDefinition} onClose={() => setLogDefinition(null)} />}
    </div>
  )
}
