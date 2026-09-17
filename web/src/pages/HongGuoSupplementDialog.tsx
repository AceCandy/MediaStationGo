import { useEffect, useRef, useState } from 'react'
import { tasksAPI } from '../api/tasks'
import { ModalShell } from '../components/ModalShell'

export function HongGuoSupplementDialog({ initialCount, onClose, onStarted }: { initialCount: number; onClose: () => void; onStarted: () => void }) {
  const [count, setCount] = useState(String(initialCount))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const pending = useRef(false)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  return <ModalShell ariaLabel="立即执行红果补充下载" maxWidth="max-w-md" className="max-h-[85dvh] overflow-y-auto p-5" onClose={busy ? undefined : onClose}>
    <form className="space-y-4" onKeyDown={(event) => {
      if (event.key !== 'Tab') return
      const controls = event.currentTarget.querySelectorAll<HTMLElement>('input:not(:disabled),button:not(:disabled)')
      const first = controls[0], last = controls[controls.length - 1]
      if (!first) { event.preventDefault(); return }
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }} onSubmit={(event) => {
      event.preventDefault()
      const value = Number(count)
      if (pending.current || !Number.isInteger(value) || value < 1 || value > 100) return
      pending.current = true; setBusy(true); setError('')
      void tasksAPI.run('hongguo_download_supplement', { count: value }).then(() => { if (active.current) { onStarted(); onClose() } }).catch((err: unknown) => { if (active.current) setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || '启动失败，请查看任务状态后重试') }).finally(() => { pending.current = false; if (active.current) setBusy(false) })
    }}>
      <h2 className="text-lg font-semibold">红果补充下载</h2>
      <p className="text-sm text-ink-50">按上线时间从新到旧选取资料齐全、从未入队的源作品。已有任务（包括失败或取消）跳过，不自动补资料。</p>
      <label className="block">本次补充数量<input autoFocus type="number" min={1} max={100} step={1} required className="input-field mt-2 w-full" value={count} disabled={busy} onChange={(event) => setCount(event.target.value)} /></label>
      <p className="text-sm text-ink-50">每次 1–100 部，只影响本次执行，不修改定时数量。后台入队结果请看任务日志，分集进度请看下载空间。</p>
      {error && <p role="alert" className="text-red-500">{error}</p>}
      <div className="flex justify-end gap-2"><button type="button" className="btn-outline" disabled={busy} onClick={onClose}>取消</button><button className="btn-primary" disabled={busy}>{busy ? '启动中…' : '确认执行'}</button></div>
    </form>
  </ModalShell>
}
