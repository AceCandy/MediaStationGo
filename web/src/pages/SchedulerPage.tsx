import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { Play } from 'lucide-react'

import { schedulerAPI, type JobStatus } from '../api/scheduler'

export function SchedulerSection() {
  const [jobs, setJobs] = useState<JobStatus[]>([])
  const [running, setRunning] = useState<string>('')

  const refresh = () => schedulerAPI.status().then(setJobs)
  useEffect(() => {
    refresh().catch(() => undefined)
    const id = window.setInterval(refresh, 5_000)
    return () => window.clearInterval(id)
  }, [])

  const runNow = async (name: string) => {
    setRunning(name)
    try {
      await schedulerAPI.run(name)
      toast.success(`${name} 已运行`)
      await refresh()
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
        '运行失败'
      toast.error(msg)
    } finally {
      setRunning('')
    }
  }

  return (
    <section id="scheduled-tasks" className="glass-panel space-y-3">
      <div>
        <h2 className="font-display text-lg font-semibold text-ink-600">定时任务</h2>
        <p className="text-sm text-ink-50">媒体库扫描等周期任务，每 5 秒刷新状态。</p>
      </div>
      <div className="overflow-x-auto">
        <table className="data-table">
          <thead>
            <tr>
              <th>任务</th>
              <th>间隔</th>
              <th>上次运行</th>
              <th>错误</th>
              <th className="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <tr key={j.name}>
                <td className="font-mono text-ink-600">{j.name}</td>
                <td className="text-ink-100">{j.interval}</td>
                <td className="text-ink-50">
                  {j.last_run && new Date(j.last_run).getFullYear() > 2000
                    ? new Date(j.last_run).toLocaleString()
                    : '尚未运行'}
                </td>
                <td className="text-red-400">{j.last_err || '—'}</td>
                <td className="text-right">
                  <button
                    onClick={() => runNow(j.name)}
                    disabled={running === j.name}
                    className="rounded-lg border border-primary-400/40 px-2 py-1 text-xs text-brand-500 hover:bg-primary-400/10"
                  >
                    <Play size={12} className="inline" /> 立即运行
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
