import { useRef, useState } from 'react'
import { LoaderCircle, MoreHorizontal, Pencil, RefreshCw } from 'lucide-react'
import toast from 'react-hot-toast'

import { api, LONG_REQUEST_TIMEOUT } from '../api/client'
import { MetadataEditDialog } from '../components/MetadataEditDialog'
import { ModalShell } from '../components/ModalShell'
import type { Media } from '../types'
import { AdminMenuItem } from './MediaDetailAdminPanel'

export function LibrarySeasonActions({ season, mediaID, episodes, onChanged }: { season: Media; mediaID: string; episodes: Media[]; onChanged: () => Promise<void> }) {
  const menu = useRef<HTMLDetailsElement>(null)
  const running = useRef(false)
  const [editing, setEditing] = useState(false)
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  const closeMenu = () => { if (menu.current) menu.current.open = false }
  const refresh = async (wholeSeason: boolean) => {
    if (running.current || !season.metadata_id) return
    closeMenu()
    running.current = true
    const ids = [...new Set([season.metadata_id, ...(wholeSeason ? episodes.map(ep => ep.metadata_id).filter((id): id is string => !!id) : [])])]
    let failed = wholeSeason ? episodes.filter(ep => !ep.metadata_id).length : 0
    setProgress({ done: 0, total: ids.length })
    const toastID = toast.loading('正在刷新季 TMDB 信息…')
    try {
      for (const [index, id] of ids.entries()) {
        try {
          await api.post(`/metadata/${encodeURIComponent(id)}/tmdb/refresh`, undefined, { timeout: LONG_REQUEST_TIMEOUT })
        } catch {
          failed += 1
        }
        setProgress({ done: index + 1, total: ids.length })
      }
      await onChanged()
      if (failed) toast.error(`刷新结束，${failed} 项未完成，可重试`, { id: toastID })
      else toast.success('季 TMDB 信息已刷新', { id: toastID })
    } catch {
      toast.error('刷新后重新加载资料失败，请重试', { id: toastID })
    } finally {
      running.current = false
      setProgress(null)
    }
  }
  return <>
    <details ref={menu} className="relative w-fit" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) closeMenu() }} onKeyDown={event => { if (event.key === 'Escape') { closeMenu(); menu.current?.querySelector('summary')?.focus() } }}>
      <summary className="btn-outline w-fit cursor-pointer list-none [&::-webkit-details-marker]:hidden"><MoreHorizontal size={16} aria-hidden="true" />整季更多操作</summary>
      <div role="menu" aria-label="季管理操作" className="absolute right-0 top-full z-50 mt-2 w-64 max-w-[calc(100vw-4rem)] overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-1.5 shadow-xl">
        <AdminMenuItem icon={RefreshCw} iconClass="text-[var(--app-gold)]" label="刷新季 TMDB 信息" disabled={!season.metadata_id} onClick={() => refresh(false)} onClose={closeMenu} />
        <AdminMenuItem icon={RefreshCw} iconClass="text-[var(--app-gold)]" label="刷新整季 TMDB 信息" disabled={!season.metadata_id} onClick={() => refresh(true)} onClose={closeMenu} title={`刷新季资料及当前 ${episodes.length} 集`} />
        <AdminMenuItem icon={Pencil} iconClass="text-[var(--app-muted)]" label="编辑季元数据" onClick={() => setEditing(true)} onClose={closeMenu} />
      </div>
    </details>
    {editing && <MetadataEditDialog open media={{ ...season, id: mediaID }} mode="season" scopeLabel="只修改当前季资料，季号、分集与文件关联保持不变。" onClose={() => setEditing(false)} onSaved={onChanged} />}
    {progress && <ModalShell ariaLabel="刷新季 TMDB 信息" className="p-6"><div className="flex items-center gap-3"><LoaderCircle size={20} className="animate-spin text-brand-500" /><span>正在刷新 {progress.done} / {progress.total}，请稍候…</span></div></ModalShell>}
  </>
}
