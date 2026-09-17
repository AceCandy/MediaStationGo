import { useEffect, useRef, useState } from 'react'
import { ModalShell } from '../components/ModalShell'
import { tasksAPI, type TaskHistory } from '../api/tasks'

const errorMessage = (err: unknown) => (err as { response?: { data?: { error?: string } } })?.response?.data?.error || '操作失败，请重试'
const stages: Record<string, string> = { downloading: '下载中', verifying: '校验中', publishing: '发布中', waiting_verify: '等待校验', queued: '等待下载', completed: '已完成', failed: '失败', cancelled: '已取消' }
const states: Record<string, string> = { running: '执行中', completed: '本阶段完成', failed: '失败', interrupted: '已中断' }

export function HongGuoDownloadActions() {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement | null>(null)
  const close = () => { setOpen(false); trigger.current?.focus() }
  return <>
    <button className="btn-outline" onClick={(event) => { trigger.current = event.currentTarget; setOpen(true) }}>执行记录</button>
    {open && <DownloadActionDialog onClose={close} />}
  </>
}

function DownloadActionDialog({ onClose }: { onClose: () => void }) {
  const [page, setPage] = useState(1)
  const [revision, setRevision] = useState(0)
  const [history, setHistory] = useState<TaskHistory | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    setHistory(null); setError('')
    void tasksAPI.history('hongguo_download', page, 20, controller.signal).then((value) => { if (!controller.signal.aborted) setHistory(value) }).catch((err) => { if (!controller.signal.aborted) setError(errorMessage(err)) })
    return () => controller.abort()
  }, [page, revision])
  return <ModalShell ariaLabel="红果下载执行记录" maxWidth="max-w-2xl" className="max-h-[85dvh] overflow-y-auto p-5" onClose={onClose}>
    <section className="space-y-4" onKeyDown={(event) => {
      if (event.key !== 'Tab') return
      const controls = event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)')
      const first = controls[0], last = controls[controls.length - 1]
      if (!first) { event.preventDefault(); return }
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }}>
      <h2 className="text-lg font-semibold">红果下载执行记录</h2>
      {error && <p role="alert" className="text-red-500">{error}</p>}
      <>
        <p className="text-sm text-ink-50">下载与校验分别记录；本阶段完成不代表文件已校验完成，最终状态请看作品任务。</p>
        <button autoFocus className="btn-outline" onClick={() => setRevision((v) => v + 1)}>刷新记录</button>
        {!history && !error && <p role="status">加载执行记录…</p>}
        {history && <>
          {history.items.length === 0 && <p>暂无执行记录。</p>}
          <ul className="space-y-3">{history.items.map((row) => <li key={row.id} className="rounded-lg border border-ink-100/10 p-3 text-sm">
            <p className="break-words font-semibold">{row.name}</p>
            <p className={row.status === 'failed' ? 'text-red-500' : row.status === 'running' ? 'text-brand-500' : 'text-ink-50'}>{states[row.status] || row.status}{row.stage && ` · ${stages[row.stage] || row.stage}`}</p>
            <p className="text-xs text-ink-50">开始：{new Date(row.started_at).toLocaleString()}{row.finished_at && ` · 结束：${new Date(row.finished_at).toLocaleString()}`}</p>
            {row.message && <p className="break-words">{row.message}</p>}
            {row.error && <p className="break-words text-red-500">{row.error}</p>}
          </li>)}</ul>
          <div className="flex flex-wrap items-center gap-3"><button className="btn-outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页记录</button><span>第 {page} 页 · 共 {history.total} 条</span><button className="btn-outline" disabled={page * 20 >= history.total} onClick={() => setPage(page + 1)}>下一页记录</button></div>
        </>}
      </>
      <button className="btn-outline" onClick={onClose}>关闭</button>
    </section>
  </ModalShell>
}
