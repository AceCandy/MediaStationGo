import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import toast from 'react-hot-toast'
import { huangguoaiAPI, huangGuoCategories, type HuangGuoAIWork } from '../api/huangguoai'
import { ModalShell } from '../components/ModalShell'
import { useAuthStore } from '../stores/auth'
import { MetadataFacts, MetadataOverview, MetadataTags } from './MediaDetailMetadata'
import { DiscoverArtworkPanel, DiscoverModalHeader } from './DiscoverDetailModalSections'

export function HuangGuoAIDetailModal({ sourceID, summary, onClose }: { sourceID: string; summary?: HuangGuoAIWork; onClose: () => void }) {
  const admin = useAuthStore(s => s.user?.role === 'admin')
  const [detail, setDetail] = useState(summary ?? null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [busy, setBusy] = useState(false)
  const [enabled, setEnabled] = useState(false)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void huangguoaiAPI.detail(sourceID, controller.signal).then(data => { if (!controller.signal.aborted) setDetail(data) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    void huangguoaiAPI.status(controller.signal).then(data => { if (!controller.signal.aborted) setEnabled(data.enabled) }).catch(() => undefined)
    return () => controller.abort()
  }, [sourceID, retry])
  const action = async (run: () => Promise<unknown>, message: string) => {
    if (busy) return
    setBusy(true)
    try { await run(); if (active.current) { toast.success(message); setRetry(v => v + 1) } }
    catch { if (active.current) toast.error('操作失败，请查看配置或任务状态后重试') }
    finally { if (active.current) setBusy(false) }
  }
  const display = { title: detail?.title || '作品详情', media_type: detail?.kind === 'movie' ? '电影' : '剧集', rating: detail?.rating, poster_url: detail?.artwork_id ? huangguoaiAPI.artwork(detail.artwork_id) : undefined }
  return <ModalShell onClose={busy ? undefined : onClose} maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto" ariaLabel="黄果AI作品详情">
    <div className="p-5 sm:p-6"><DiscoverModalHeader item={display} source="黄果 AI" onClose={() => { if (!busy) onClose() }} />
      <div className="grid gap-5 lg:grid-cols-[260px_1fr]"><DiscoverArtworkPanel item={display} /><div className="min-w-0 space-y-4">
        {error && <p role="alert">完整资料暂不可用 <button className="btn-outline" disabled={busy} onClick={() => setRetry(v => v + 1)}>重试</button></p>}
        {detail && <><MetadataFacts rating={detail.rating} date={detail.source_created_at || '未知'} dateLabel="来源时间" /><p className="text-sm text-ink-50">源 ID：{sourceID} · {huangGuoCategories.find(([key]) => key === detail.source_category)?.[1] || '分类待补齐'} · 来源已更新 {detail.episode_count || '未知'} 集</p><p className="text-xs text-ink-50">来源时间不代表全网首播时间。</p><MetadataOverview overview={detail.overview || '暂无简介'} /><MetadataTags label="题材标签" values={detail.tags ?? []} primary />{detail.projection_error && <p role="alert">分类存在冲突，暂停下载与播放投影。</p>}</>}
        <div className="flex flex-wrap gap-2">{admin && <><button className="btn-outline" disabled={busy || !enabled} onClick={() => void action(() => huangguoaiAPI.refresh(sourceID), '资料已刷新')}>刷新资料</button><button className="btn-primary" disabled={busy || !enabled || !detail?.hydrated || !!detail?.projection_error} onClick={() => void action(() => huangguoaiAPI.enqueue(sourceID), '已加入下载队列')}>下载分集</button><Link className="btn-outline" to="/admin/media/downloads?source=huangguoai">下载空间</Link></>}</div>
      </div></div>
    </div>
  </ModalShell>
}
