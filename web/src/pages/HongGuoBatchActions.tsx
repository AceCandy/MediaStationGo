import { useEffect, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { hongguoAPI, type HongGuoListWork } from '../api/hongguo'
import { hongguoDownloadsAPI } from '../api/hongguoDownloads'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'
import { motion, Reorder, useDragControls, useReducedMotion } from 'framer-motion'
import { GripVertical } from 'lucide-react'

export function HongGuoBatchActions({ selected, onSelectedChange, enabled, busy, onBusyChange, onGroupSaved }: {
  selected: HongGuoListWork[]; onSelectedChange: (works: HongGuoListWork[]) => void
  enabled: boolean; busy: boolean; onBusyChange: (value: boolean) => void
  onGroupSaved: (id: string, works: HongGuoListWork[]) => void
}) {
  const active = useRef(true)
  const submitting = useRef(false)
  const groupButton = useRef<HTMLButtonElement>(null)
  const selectionCount = useRef<HTMLSpanElement>(null)
  const [members, setMembers] = useState<HongGuoListWork[] | null>(null)
  const [title, setTitle] = useState('')
  const [message, setMessage] = useState('')
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  const close = () => { setMembers(null); groupButton.current?.focus() }
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
  const saveGroup = async () => {
    if (submitting.current || !members || members.length < 2 || !title.trim()) return
    submitting.current = true; onBusyChange(true)
    try {
      const result = await hongguoAPI.saveGroup(undefined, title.trim(), members.map((work, index) => ({ source_id: work.source_id, season_number: index + 1 })))
      if (!active.current) return
      onGroupSaved(result.data.id, members)
      onSelectedChange([]); setMembers(null); selectionCount.current?.focus(); setMessage('聚合已保存，媒体库将按确认的季序展示'); toast.success('聚合已保存')
    } catch { if (active.current) toast.error('聚合失败：请确认作品资料完整、不是电影且未属于其他聚合') }
    finally { submitting.current = false; if (active.current) onBusyChange(false) }
  }
  const move = (index: number, offset: number) => setMembers((current) => {
    if (!current || index + offset < 0 || index + offset >= current.length) return current
    const next = [...current]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; return next
  })
  const canGroup = selected.length >= 2 && selected.length <= 1000 && selected.every((work) => work.kind === 'series')
  return <>
    <span ref={selectionCount} tabIndex={-1} className="text-sm" aria-live="polite">已选 {selected.length} 部</span>
    <button type="button" className="btn-primary" disabled={busy || !enabled || !selected.length} onClick={() => void download()}>下载所选</button>
    <button ref={groupButton} type="button" className="btn-outline" disabled={busy || !canGroup} onClick={() => { setMembers([...selected]); setTitle(selected[0].title) }}>聚合所选</button>
    <button type="button" className="btn-outline" disabled={busy || !selected.length} onClick={() => onSelectedChange([])}>清空选择</button>
    {selected.some((work) => work.kind !== 'series') && <span className="text-xs text-ink-50">电影不能作为聚合季</span>}
    {message && <p role="status" className="w-full break-words text-sm text-ink-50">{message}</p>}
    {members && <ModalShell maxWidth="max-w-2xl" className="max-h-[85dvh] overflow-y-auto p-5 sm:p-6" ariaLabel="确认跨季聚合">
      <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); void saveGroup() }} onKeyDown={(event) => {
        if (event.key !== 'Tab') return
        const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)'))
        const first = controls[0], last = controls[controls.length - 1]
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
      }}>
        <h2 className="text-xl font-semibold">确认跨季聚合</h2>
        <label className="block text-sm">聚合剧名<input autoFocus required className="input-field mt-2 w-full" value={title} disabled={busy} onChange={(event) => setTitle(event.target.value)} /></label>
        <p className="text-sm text-ink-50">按下列顺序作为同一部剧的多季。聚合只改变逻辑展示，不改文件、下载目录或观看进度。</p>
        <motion.div layoutScroll className="max-h-[50dvh] overflow-y-auto p-1"><Reorder.Group as="ol" axis="y" values={members} onReorder={(next) => { if (!busy) setMembers(next) }} className="space-y-2">{members.map((work, index) => <GroupMember key={work.source_id} work={work} index={index} count={members.length} busy={busy} move={move} />)}</Reorder.Group></motion.div>
        <div className="flex justify-end gap-2"><button type="button" className="btn-outline" disabled={busy} onClick={close}>取消</button><button className="btn-primary" disabled={busy || !title.trim()}>{busy ? '保存中…' : '确认聚合'}</button></div>
      </form>
    </ModalShell>}
  </>
}

function GroupMember({ work, index, count, busy, move }: { work: HongGuoListWork; index: number; count: number; busy: boolean; move: (index: number, offset: number) => void }) {
  const controls = useDragControls()
  const reduced = useReducedMotion()
  return <Reorder.Item value={work} dragListener={false} dragControls={controls} layout="position" transition={reduced ? { duration: 0 } : undefined} className="relative flex items-center gap-2 rounded-xl border border-ink-100/10 bg-[var(--app-bg)] p-2">
    <button type="button" className="shrink-0 touch-none cursor-grab px-1 py-4 text-ink-50 active:cursor-grabbing" disabled={busy} aria-label={`拖动${work.title}调整季序`} onPointerDown={(event) => { if (!busy) controls.start(event) }} onKeyDown={(event) => { if (event.key === 'ArrowUp' || event.key === 'ArrowDown') { event.preventDefault(); move(index, event.key === 'ArrowUp' ? -1 : 1) } }}><GripVertical size={20} /></button>
    {work.artwork_id ? <img draggable={false} className="h-16 w-11 shrink-0 rounded object-cover" loading="lazy" src={imageURL(hongguoAPI.artwork(work.artwork_id))} alt={`${work.title}海报`} /> : <div className="flex h-16 w-11 shrink-0 items-center justify-center rounded bg-gray-100 text-xs text-ink-50">无海报</div>}
    <div className="min-w-0 flex-1"><p className="text-sm font-semibold text-brand-500">第 {index + 1} 季</p><p className="break-words text-sm">{work.title}</p><p className="break-all text-xs text-ink-50">{work.source_id}</p></div>
    <div className="flex shrink-0 flex-col gap-1"><button type="button" className="btn-outline px-2 py-1" aria-label={`上移${work.title}`} disabled={busy || index === 0} onClick={() => move(index, -1)}>上移</button><button type="button" className="btn-outline px-2 py-1" aria-label={`下移${work.title}`} disabled={busy || index === count - 1} onClick={() => move(index, 1)}>下移</button></div>
  </Reorder.Item>
}
