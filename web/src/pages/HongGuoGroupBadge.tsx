import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link2 } from 'lucide-react'
import { hongguoAPI, type HongGuoGroup } from '../api/hongguo'

export function HongGuoGroupBadge({ groupID, sourceID }: { groupID: string; sourceID: string }) {
  const [position, setPosition] = useState<{ top: number; left: number; above: boolean } | null>(null)
  const [group, setGroup] = useState<HongGuoGroup | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const button = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const stay = () => clearTimeout(leaveTimer.current)
  const leave = () => {
    stay()
    if (panel.current?.contains(document.activeElement) || button.current === document.activeElement) return
    leaveTimer.current = setTimeout(() => setPosition(null), 150)
  }
  useEffect(() => () => clearTimeout(leaveTimer.current), [])
  const open = () => {
    stay()
    const rect = button.current?.getBoundingClientRect()
    if (rect) {
      const above = window.innerHeight - rect.bottom < 200 && rect.top > window.innerHeight - rect.bottom
      setPosition({ top: Math.max(8, above ? rect.top - 6 : rect.bottom + 6), left: Math.max(8, Math.min(rect.left, window.innerWidth - 288)), above })
    }
  }
  const visible = position !== null
  useEffect(() => {
    if (!visible || group) return
    const controller = new AbortController()
    setError(false)
    void hongguoAPI.group(groupID, controller.signal).then((value) => { if (!controller.signal.aborted) setGroup(value) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [visible, group, groupID, retry])
  useEffect(() => {
    if (!visible) return
    const close = (event: Event) => {
      if (event.target instanceof Node && (panel.current?.contains(event.target) || button.current?.contains(event.target))) return
      setPosition(null)
    }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { button.current?.focus(); setPosition(null) } }
    window.addEventListener('scroll', close, true); window.addEventListener('resize', close); window.addEventListener('pointerdown', close); window.addEventListener('keydown', escape)
    return () => { window.removeEventListener('scroll', close, true); window.removeEventListener('resize', close); window.removeEventListener('pointerdown', close); window.removeEventListener('keydown', escape) }
  }, [visible])
  return <div className="pointer-events-auto shrink-0" onMouseEnter={open} onMouseLeave={leave}>
    <button ref={button} type="button" aria-label="已关联剧集" aria-expanded={visible} aria-controls={`group-${sourceID}`} className="flex h-6 w-6 items-center justify-center rounded text-white focus-visible:ring-2 focus-visible:ring-brand-500" onFocus={open} onClick={() => { open(); requestAnimationFrame(() => panel.current?.focus()) }} onKeyDown={(event) => { if ((event.key === 'Tab' && !event.shiftKey || event.key === 'ArrowDown') && panel.current) { event.preventDefault(); panel.current.focus() } }} onBlur={(event) => { if (!panel.current?.contains(event.relatedTarget)) setPosition(null) }}><Link2 className="h-3.5 w-3.5" aria-hidden="true" /></button>
    {position && createPortal(<div ref={panel} id={`group-${sourceID}`} tabIndex={0} style={{ top: position.top, left: position.left, transform: position.above ? 'translateY(-100%)' : undefined, maxHeight: Math.min(192, Math.max(0, position.above ? position.top - 8 : window.innerHeight - position.top - 8)) }} className="fixed z-50 w-[280px] max-w-[calc(100vw-16px)] overflow-y-auto rounded-xl border border-ink-100/10 bg-[var(--app-bg)] p-3 text-sm shadow-xl" onMouseEnter={stay} onMouseLeave={leave} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setPosition(null) }}>
      {error ? <p role="alert">关联资料读取失败 <button className="text-brand-500" onClick={() => setRetry((value) => value + 1)}>重试</button></p> : !group ? <p role="status">加载关联剧集…</p> : <><p className="mb-2 break-words font-semibold">{group.title}</p><ul className="space-y-2">{group.members.filter((work) => work.source_id !== sourceID).map((work) => <li key={work.source_id} className="break-words"><span className="mr-2 text-brand-500">第 {work.season_number} 季</span>{work.title}</li>)}</ul>{group.members.every((work) => work.source_id === sourceID) && <p className="text-ink-50">暂无其他关联作品</p>}</>}
    </div>, document.body)}
  </div>
}
