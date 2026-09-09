import { useEffect, useRef, useState } from 'react'
import { LoaderCircle, Search, X } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI, type ManualScrapeCandidate } from '../api/library'
import type { Media } from '../types'
import { ModalShell } from './ModalShell'
import { ConfirmDialog } from './ConfirmDialog'
import { ManualScrapeCandidateList } from './ManualScrapeDialogSections'
import { candidateKey } from './ManualScrapeDialogModel'

export function DoubanBindingDialog({ media, onClose, onBound }: { media: Media; onClose: () => void; onBound: () => void | Promise<void> }) {
  const [query, setQuery] = useState(media.title)
  const [search, setSearch] = useState({ query: media.title, revision: 0 })
  const [items, setItems] = useState<ManualScrapeCandidate[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [applying, setApplying] = useState('')
  const [confirm, setConfirm] = useState<ManualScrapeCandidate | null>(null)
  const pending = useRef(false)
  const mounted = useRef(true)
  const localType = media.metadata_kind === 'series' ? 'tv' : 'movie'
  const metadataID = media.metadata_id

  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setItems([])
    setError('')
    if (!metadataID) { setError('当前作品尚未绑定元数据'); setLoading(false); return () => controller.abort() }
    mediaAPI.searchDoubanBinding(metadataID, search.query, controller.signal)
      .then((result) => { if (!controller.signal.aborted) setItems(result) })
      .catch((err: unknown) => { if (!controller.signal.aborted) setError((err as { response?: { data?: { error?: string } } }).response?.data?.error || '豆瓣搜索失败，请重试') })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [metadataID, search])

  const apply = async (item: ManualScrapeCandidate, force = false) => {
    if (pending.current || !metadataID || !item.douban_id || (item.media_type !== 'movie' && item.media_type !== 'tv')) return
    if (item.media_type !== localType && !force) { setConfirm(item); return }
    pending.current = true
    setApplying(candidateKey(item))
    setError('')
    try {
      const result = await mediaAPI.bindDouban(metadataID, { douban_id: item.douban_id, media_type: item.media_type, force })
      if (!mounted.current) return
      toast.success(result.status === 'degraded' ? '豆瓣已绑定并补齐可用信息，完整接口受限' : '豆瓣已绑定并补齐信息')
      try { await onBound() } catch { toast.error('绑定已保存，页面刷新失败，请刷新重试') }
      onClose()
    } catch (err: unknown) {
      if (mounted.current) setError((err as { response?: { data?: { error?: string } } }).response?.data?.error || '豆瓣绑定失败，请重试')
    } finally {
      pending.current = false
      if (mounted.current) setApplying('')
    }
  }

  return <>
    <ModalShell onClose={!applying && !confirm ? onClose : undefined} maxWidth="max-w-3xl" className="flex max-h-[85vh] flex-col" ariaLabel="绑定豆瓣信息">
      <div className="modal-header">
        <div><h2 className="font-display text-lg font-bold">绑定豆瓣信息</h2><p className="mt-1 text-xs text-[var(--app-muted)]">应用后立即补齐当前{localType === 'tv' ? '整剧' : '电影'}，保留已有信息和海报。</p></div>
        <button type="button" className="icon-btn" aria-label="关闭豆瓣匹配" disabled={!!applying || !!confirm} onClick={onClose}><X size={16} /></button>
      </div>
      <form className="flex gap-2 px-5 pt-4" onSubmit={(event) => { event.preventDefault(); if (!pending.current && query.trim()) setSearch({ query: query.trim(), revision: search.revision + 1 }) }}>
        <input aria-label="豆瓣搜索关键词" placeholder="作品名、豆瓣链接或 ID" className="input-field min-w-0 flex-1" value={query} maxLength={200} disabled={!!applying} onChange={(event) => setQuery(event.target.value)} />
        <button type="submit" className="btn-primary shrink-0" disabled={!!applying || loading || !query.trim()}><Search size={16} />搜索</button>
      </form>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-5">
        <p className="text-xs text-[var(--app-muted)]">展示完整搜索的前 5 个作品；可输入更准确的名称或豆瓣链接 / ID。</p>
        {error && <p role="alert" className="text-sm text-red-500">{error}</p>}
        {items.some(item => item.media_type !== 'movie' && item.media_type !== 'tv') && <p role="status" className="text-sm text-[var(--app-muted)]">部分条目的详情获取失败或类型无法确认，暂不可应用，请稍后重新搜索。</p>}
        {loading ? <p role="status" className="flex items-center gap-2 py-8"><LoaderCircle size={18} className="animate-spin" />正在搜索并确认豆瓣类型…</p> : items.length ? <ManualScrapeCandidateList items={items} applyingKey={applying} onApply={(item) => void apply(item)} requireDoubanType /> : !error && <p className="py-8 text-sm text-[var(--app-muted)]">没有找到豆瓣条目</p>}
        {applying && <p role="status" className="text-sm">正在获取豆瓣详情和海报并保存，请稍候…</p>}
      </div>
      {media.douban_id && <div className="modal-footer"><a className="btn-ghost" href={`https://movie.douban.com/subject/${encodeURIComponent(media.douban_id)}/`} target="_blank" rel="noopener noreferrer">打开当前豆瓣条目</a></div>}
    </ModalShell>
    {confirm && <ConfirmDialog options={{ title: '豆瓣类型不一致', message: `当前为${localType === 'tv' ? '整剧' : '电影'}，所选《${confirm.title}》是豆瓣${confirm.media_type === 'tv' ? '电视剧' : '电影'}。强制绑定将立即补齐信息，但不会改变本地类型、季集编号和文件归属。`, confirmText: '强制绑定并补齐', danger: false }} onClose={(accepted) => { const item = confirm; setConfirm(null); if (accepted) void apply(item, true) }} />}
  </>
}
