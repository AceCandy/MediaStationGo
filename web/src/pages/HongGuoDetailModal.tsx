import { useEffect, useRef, useState } from 'react'
import toast from 'react-hot-toast'
import { hongguoAPI, type HongGuoDetail, type HongGuoListWork } from '../api/hongguo'
import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import { useAuthStore } from '../stores/auth'
import { HongGuoDownloadButton } from './HongGuoDownloadButton'
import { MetadataFacts, MetadataOverview, MetadataTags } from './MediaDetailMetadata'
import { MediaCredits } from './MediaDetailCast'
import { DiscoverArtworkPanel, DiscoverModalHeader } from './DiscoverDetailModalSections'

export function HongGuoDetailModal({ sourceID, summary, enabled, onClose, onCategorySaved }: { sourceID: string; summary?: HongGuoListWork; enabled: boolean; onClose: () => void; onCategorySaved: (category: string) => void }) {
  const admin = useAuthStore((s) => s.user?.role === 'admin')
  const [detail, setDetail] = useState<HongGuoDetail | null>(() => summary ? { ...summary, tags: summary.tags ?? [], episodes: [], credits: [], artwork: summary.artwork_id ? [{ id: summary.artwork_id, work_id: summary.id }] : [] } : null)
  const [error, setError] = useState(false)
  const [revision, setRevision] = useState(0)
  const [busy, setBusy] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    const opener = document.activeElement
    contentRef.current?.querySelector<HTMLButtonElement>('button[aria-label="关闭详情"]')?.focus()
    return () => { active.current = false; if (opener instanceof HTMLElement) opener.focus() }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void hongguoAPI.detail(sourceID, controller.signal).then((data) => {
      if (!controller.signal.aborted) setDetail(data)
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [sourceID, revision])
  const update = async (action: () => Promise<unknown>, message: string) => {
    if (busy) return
    setBusy(true)
    try { await action(); if (active.current) toast.success(message) }
    catch { if (active.current) toast.error('操作失败，请重试') }
    finally { if (active.current) setBusy(false) }
  }
  const poster = detail?.artwork.find((a) => a.work_id === detail.id)
  const display = { title: detail?.title || '作品详情', media_type: detail?.kind === 'movie' ? '电影' : '剧集', rating: detail?.rating, poster_url: poster ? hongguoAPI.artwork(poster.id) : undefined }
  const credits = (detail?.credits ?? []).map((credit) => {
    const avatar = detail?.artwork.find((a) => a.person_id === credit.person_id)
    return { person_id: credit.person_id, name: credit.person.name, role: credit.subtitle, type: '', profile_url: avatar ? hongguoAPI.artwork(avatar.id) : undefined }
  })
  return <ModalShell onClose={onClose} maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto" ariaLabel={detail?.title || '红果作品详情'}>
    <div ref={contentRef} className="relative isolate min-h-full p-5 sm:p-6" onKeyDown={(event) => {
      if (event.key !== 'Tab') return
      const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled), [tabindex="0"]')).filter((node) => node.getClientRects().length > 0)
      const first = controls[0], last = controls[controls.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }}>
      <DiscoverModalHeader item={display} source="红果短剧" onClose={onClose} />
      <div className="grid gap-5 lg:grid-cols-[260px_1fr]">
        <DiscoverArtworkPanel item={display} />
        <div className="min-w-0 space-y-5">
          {error && <div role="alert" className="flex flex-wrap items-center gap-3 text-sm"><span>完整资料暂时不可用</span><button className="btn-outline" onClick={() => setRevision((v) => v + 1)}>重试详情</button></div>}
          {!detail && !error && <p role="status" className="text-sm text-[var(--app-muted)]">正在加载完整资料…</p>}
          {detail && <>
            <MetadataFacts rating={detail.rating ?? 0} date={detail.first_visible_at ? new Date(detail.first_visible_at).toLocaleDateString() : '未知'} dateLabel="红果上线" />
            <div className="space-y-2 text-sm text-[var(--app-muted)]">
              <p className="break-all">红果 ID：{detail.source_id} · {detail.update_text || `已更新 ${detail.episode_count} 集`}</p>
              <p>红果上线时间不代表全网首播时间。{detail.rating_count > 0 && `评分人数：${detail.rating_count}`}</p>
            </div>
            <MetadataOverview overview={detail.overview || '暂无简介'} />
            <MetadataTags label="类型流派" values={detail.tags} primary />
            {credits.length ? <MediaCredits credits={credits} /> : <p className="text-sm text-[var(--app-muted)]">暂无演职员资料</p>}
        <div className="flex flex-wrap items-center gap-3">
          {admin && <><HongGuoDownloadButton sourceID={sourceID} enabled={enabled} />
            {(!detail.source_category || detail.source_category === 'other') && <Select aria-label="设置作品分类" className="input-field w-36" value={detail.source_category || 'other'} disabled={busy} onChange={(category) => void update(async () => { await hongguoAPI.setSourceCategory(sourceID, category); if (active.current) { setDetail({ ...detail, source_category: category }); onCategorySaved(category) } }, '分类已保存')}><option value="other" disabled>其它</option><option value="real-drama">真人剧</option><option value="comic-drama">漫剧</option><option value="ai-drama">AI剧</option></Select>}
          </>}
        </div>
          </>}
        </div>
      </div>
    </div>
  </ModalShell>
}
