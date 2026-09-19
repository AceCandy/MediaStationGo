import { useEffect, useRef, useState } from 'react'
import { type HongGuoListWork } from '../api/hongguo'
import { hongguoDownloadsAPI } from '../api/hongguoDownloads'

export function HongGuoBatchActions({ selected, onSelectedChange, enabled, busy, onBusyChange }: {
  selected: HongGuoListWork[]; onSelectedChange: (works: HongGuoListWork[]) => void
  enabled: boolean; busy: boolean; onBusyChange: (value: boolean) => void
}) {
  const active = useRef(true)
  const submitting = useRef(false)
  const [message, setMessage] = useState('')
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  const download = async () => {
    if (submitting.current || !enabled || !selected.length) return
    submitting.current = true; onBusyChange(true); setMessage('正在提交下载…')
    const failed: HongGuoListWork[] = []
    let added = 0
    try {
      const config = await hongguoDownloadsAPI.config()
      if (!active.current) return
      if (!config.root) { setMessage('请先在下载空间设置存储目录'); return }
      for (const work of selected) {
        if (!active.current) return
        try { const result = await hongguoDownloadsAPI.enqueue(work.source_id); added += result.added }
        catch { failed.push(work) }
      }
      if (!active.current) return
      onSelectedChange(failed)
      setMessage(`已提交 ${selected.length - failed.length} 部，新增 ${added} 个分集任务${failed.length ? `；${failed.length} 部失败，已保留勾选，可重试` : '；已有任务不重复添加'}`)
    } catch { if (active.current) setMessage('下载设置读取失败，请重试') }
    finally { submitting.current = false; if (active.current) onBusyChange(false) }
  }
  return <>
    <span className="text-sm" aria-live="polite">已选 {selected.length} 部</span>
    <button type="button" className="btn-primary" disabled={busy || !enabled || !selected.length} onClick={() => void download()}>下载所选</button>
    <button type="button" className="btn-outline" disabled={busy || !selected.length} onClick={() => onSelectedChange([])}>清空选择</button>
    {message && <p role="status" className="w-full break-words text-sm text-ink-50">{message}</p>}
  </>
}
