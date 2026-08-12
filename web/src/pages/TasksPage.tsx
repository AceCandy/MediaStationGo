import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Activity, ChevronLeft, ChevronRight, Clock3, Copy, FileText, X } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import { tasksAPI, type BackgroundTask, type TaskLog, type TasksSnapshot } from '../api/tasks'
import { SchedulerSection } from './SchedulerPage'

const PAGE_SIZE = 30

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

function hasTaskIssues(task: BackgroundTask): boolean {
  return Boolean(task.metrics?.errors || task.metrics?.scan_errors || task.metrics?.scrape_errors || task.metrics?.failed)
}

function StatusBadge({ task }: { task: BackgroundTask }) {
  if (task.status === 'failed') return <span className="rounded border border-red-300 px-1.5 py-0.5 text-xs text-red-600">失败</span>
  if (task.status === 'interrupted') return <span className="rounded border border-gray-300 px-1.5 py-0.5 text-xs text-gray-600">中断</span>
  if (hasTaskIssues(task)) return <span className="rounded border border-orange-300 px-1.5 py-0.5 text-xs text-orange-600">有异常</span>
  if (task.status === 'completed') return <span className="rounded border border-emerald-300 px-1.5 py-0.5 text-xs text-emerald-600">完成</span>
  return <span className="rounded border border-yellow-300 px-1.5 py-0.5 text-xs text-yellow-700">运行中</span>
}

function taskCopyText(task: BackgroundTask): string {
  const lines = [
    `任务: ${task.name}`,
    `来源: ${triggerLabels[task.trigger] ?? task.trigger}`,
    `状态: ${task.status}`,
    `阶段: ${task.stage || '-'}`,
    `消息: ${task.error || task.message || '-'}`,
  ]
  const metrics = formatMetrics(task.metrics)
  if (metrics) lines.push(`指标: ${metrics}`)
  return lines.join('\n')
}

async function copyTask(task: BackgroundTask) {
  try {
    await navigator.clipboard.writeText(taskCopyText(task))
    toast.success('任务摘要已复制')
  } catch {
    toast.error('复制失败')
  }
}

function TaskTable({ tasks, onLog }: { tasks: BackgroundTask[]; onLog: (task: BackgroundTask) => void }) {
  if (tasks.length === 0) return <p className="py-8 text-center text-sand-500">暂无任务执行记录。</p>
  return (
    <table className="w-full min-w-[880px] text-left text-sm">
      <thead className="text-xs uppercase tracking-wider text-sand-500">
        <tr><th className="py-2">任务</th><th>来源</th><th>阶段</th><th>状态</th><th>结果</th><th>时间</th><th className="text-right">操作</th></tr>
      </thead>
      <tbody>
        {tasks.map((task) => (
          <tr key={task.id} className="border-t border-gray-200 align-top">
            <td className="max-w-xs py-3">
              <div className="font-medium text-ink-600">{task.name}</div>
              <div className="truncate text-xs text-sand-500" title={task.source_path || task.dest_path}>{task.source_path || task.dest_path || '-'}</div>
            </td>
            <td className="text-ink-100">{triggerLabels[task.trigger] ?? task.trigger}</td>
            <td className="text-ink-100">{task.stage || '-'}</td>
            <td><StatusBadge task={task} /></td>
            <td className="max-w-sm text-ink-100">
              <div className="break-words">{task.error || task.message || '-'}</div>
              {formatMetrics(task.metrics) && <div className="mt-1 text-xs text-sand-500">{formatMetrics(task.metrics)}</div>}
            </td>
            <td className="whitespace-nowrap text-ink-100">{new Date(task.finished_at || task.updated_at || task.started_at).toLocaleString()}</td>
            <td className="whitespace-nowrap py-3 text-right">
              <button type="button" className="mr-1 rounded border border-gray-200 p-1.5 text-sand-500 hover:text-brand-500" title="查看任务日志" onClick={() => onLog(task)}><FileText size={15} /></button>
              <button type="button" className="rounded border border-gray-200 p-1.5 text-sand-500 hover:text-brand-500" title="复制任务摘要" onClick={() => void copyTask(task)}><Copy size={15} /></button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function TaskLogDialog({ task, onClose }: { task: BackgroundTask; onClose: () => void }) {
  const [log, setLog] = useState<TaskLog | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let active = true
    tasksAPI.log(task.id).then((value) => active && setLog(value)).catch(() => active && setError('任务日志读取失败'))
    return () => { active = false }
  }, [task.id])
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-4 backdrop-blur-sm" onClick={onClose}>
      <section role="dialog" aria-modal="true" aria-label={`${task.name}日志`} className="glass-panel flex max-h-[85vh] w-full max-w-4xl flex-col gap-3" onClick={(event) => event.stopPropagation()}>
        <header className="flex items-start justify-between gap-3">
          <div className="min-w-0"><h2 className="truncate font-display text-lg font-semibold text-ink-600">{task.name}</h2><p className="text-xs text-ink-50">{triggerLabels[task.trigger]} · {task.status}</p></div>
          <button type="button" className="rounded p-1.5 text-sand-500 hover:bg-gray-100" title="关闭" onClick={onClose}><X size={18} /></button>
        </header>
        {error ? <p className="text-sm text-red-500">{error}</p> : !log ? <p className="text-sm text-sand-500">加载中...</p> : (
          <><pre className="min-h-40 flex-1 overflow-auto whitespace-pre-wrap break-words rounded border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-relaxed text-gray-100">{log.content || '暂无详细日志。'}</pre>{log.truncated && <p className="text-xs text-orange-600">日志过长，当前显示末尾内容。</p>}</>
        )}
      </section>
    </div>
  )
}

export function TasksPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [snap, setSnap] = useState<TasksSnapshot | null>(null)
  const [page, setPage] = useState(1)
  const [logTask, setLogTask] = useState<BackgroundTask | null>(null)
  const [schedulerOpen, setSchedulerOpen] = useState(searchParams.get('panel') === 'scheduler')

  useEffect(() => {
    let cancelled = false
    const tick = () => tasksAPI.snapshot(page, PAGE_SIZE)
      .then((value) => { if (!cancelled) setSnap(value) })
      .catch(() => undefined)
    void tick()
    const id = window.setInterval(tick, 3_000)
    return () => { cancelled = true; window.clearInterval(id) }
  }, [page])

  const pages = Math.max(1, Math.ceil((snap?.total ?? 0) / PAGE_SIZE))
  useEffect(() => {
    if (page > pages) setPage(pages)
  }, [page, pages])

  const closeScheduler = () => {
    setSchedulerOpen(false)
    if (searchParams.has('panel')) {
      const next = new URLSearchParams(searchParams)
      next.delete('panel')
      setSearchParams(next, { replace: true })
    }
  }
  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3"><Activity className="h-6 w-6 text-brand-500" /><div><h1 className="font-display text-3xl font-bold text-ink-600">任务中心</h1><p className="text-sm text-ink-50">统一查看手动、定时和事件触发的后台执行。</p></div></div>
        <button type="button" className="btn-outline gap-2" onClick={() => setSchedulerOpen(true)}><Clock3 size={16} />周期任务</button>
      </header>
      <section className="glass-panel overflow-x-auto">
        {!snap ? <p className="py-8 text-center text-sand-500">加载中...</p> : <TaskTable tasks={snap.items ?? []} onLog={setLogTask} />}
      </section>
      <nav className="flex items-center justify-end gap-2 text-sm text-ink-100" aria-label="任务分页">
        <button type="button" className="rounded border border-gray-200 p-1.5 disabled:opacity-40" title="上一页" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}><ChevronLeft size={16} /></button>
        <span>{page} / {pages}</span>
        <button type="button" className="rounded border border-gray-200 p-1.5 disabled:opacity-40" title="下一页" disabled={page >= pages} onClick={() => setPage((value) => value + 1)}><ChevronRight size={16} /></button>
      </nav>
      {logTask && <TaskLogDialog task={logTask} onClose={() => setLogTask(null)} />}
      {schedulerOpen && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/35 p-4 backdrop-blur-sm" onClick={closeScheduler}><div role="dialog" aria-modal="true" aria-label="周期任务管理" className="w-full max-w-4xl" onClick={(event) => event.stopPropagation()}><div className="mb-2 flex justify-end"><button type="button" className="rounded bg-white p-1.5 text-sand-500 shadow" title="关闭" onClick={closeScheduler}><X size={18} /></button></div><SchedulerSection /></div></div>}
    </div>
  )
}
