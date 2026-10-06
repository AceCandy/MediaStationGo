import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import toast from 'react-hot-toast'
import { huangguoaiAPI, huangGuoCategories, type HuangGuoAIWork } from '../api/huangguoai'
import type { Media } from '../types'
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
  const [files, setFiles] = useState<Media[]>([])
  const [fileTotal, setFileTotal] = useState(0)
  const [filePage, setFilePage] = useState(1)
  const [filesError, setFilesError] = useState(false)
  const [episodes, setEpisodes] = useState<number[]>([])
  const [episodePage, setEpisodePage] = useState(1)
  const [episodeTotal, setEpisodeTotal] = useState(0)
  const [episodesError, setEpisodesError] = useState(false)
  const [favorite, setFavorite] = useState(false)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void huangguoaiAPI.detail(sourceID, controller.signal).then(data => { if (!controller.signal.aborted) setDetail(data) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    void huangguoaiAPI.status(controller.signal).then(data => { if (!controller.signal.aborted) setEnabled(data.enabled) }).catch(() => undefined)
    void huangguoaiAPI.state(sourceID, controller.signal).then(data => { if (!controller.signal.aborted) setFavorite(data.favorite) }).catch(() => undefined)
    return () => controller.abort()
  }, [sourceID, retry])
  useEffect(() => {
    const controller = new AbortController(); setFilesError(false)
    void huangguoaiAPI.media(sourceID, filePage, controller.signal).then(data => { if (!controller.signal.aborted) { setFiles(data.items); setFileTotal(data.total) } }).catch(() => { if (!controller.signal.aborted) setFilesError(true) })
    return () => controller.abort()
  }, [sourceID, filePage, retry])
  useEffect(() => {
    const controller = new AbortController(); setEpisodesError(false)
    void huangguoaiAPI.episodes(sourceID, episodePage, controller.signal).then(data => { if (!controller.signal.aborted) { setEpisodes(data.items.map(ep => ep.number)); setEpisodeTotal(data.total) } }).catch(() => { if (!controller.signal.aborted) setEpisodesError(true) })
    return () => controller.abort()
  }, [sourceID, episodePage, retry])
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
        {detail && <><MetadataFacts rating={detail.rating} date={detail.source_created_at || '未知'} dateLabel="来源时间" /><p className="text-sm text-ink-50">源 ID：{sourceID} · {huangGuoCategories.find(([key]) => key === detail.source_category)?.[1] || '分类待补齐'} · 来源已更新 {detail.episode_count || '未知'} 集 · 已确认 {detail.confirmed_episode_count || 0} 集</p><p className="text-xs text-ink-50">来源时间不代表全网首播时间。</p><MetadataOverview overview={detail.overview || '暂无简介'} /><MetadataTags label="题材标签" values={detail.tags ?? []} primary />{detail.projection_error && <p role="alert">分类存在冲突，暂停下载与播放投影。</p>}</>}
        <div className="flex flex-wrap gap-2">{admin && <><button className="btn-outline" disabled={busy || !enabled} onClick={() => void action(() => huangguoaiAPI.refresh(sourceID), '资料已刷新')}>刷新资料</button><button className="btn-primary" disabled={busy || !enabled || !detail?.hydrated || !!detail?.projection_error} onClick={() => void action(() => huangguoaiAPI.enqueue(sourceID), '已加入下载队列')}>下载已确认分集</button><Link className="btn-outline" to="/admin/media/downloads?source=huangguoai">下载空间</Link></>}{fileTotal > 0 && <button className="btn-outline" disabled={busy} onClick={() => void action(() => huangguoaiAPI.setFavorite(sourceID, !favorite), favorite ? '已取消收藏' : '已收藏')}>{favorite ? '取消收藏' : '收藏作品'}</button>}</div>
        <div className="space-y-2"><h3 className="font-semibold">已确认分集</h3>{episodesError ? <p>分集资料未补齐或读取失败 <button className="btn-outline" onClick={() => setRetry(v => v + 1)}>重试</button></p> : <><p className="break-words text-sm">{episodes.map(number => `第 ${number} 集`).join(' · ') || '暂无'}</p>{episodeTotal > 100 && <div className="flex gap-2"><button className="btn-outline" disabled={episodePage === 1} onClick={() => setEpisodePage(v => v - 1)}>上一页分集</button><button className="btn-outline" disabled={episodePage * 100 >= episodeTotal} onClick={() => setEpisodePage(v => v + 1)}>下一页分集</button></div>}</>}</div>
        <div className="space-y-2"><h3 className="font-semibold">本地文件</h3>{filesError ? <p role="alert">文件读取失败 <button className="btn-outline" onClick={() => setRetry(v => v + 1)}>重试</button></p> : files.length ? <><div className="flex flex-wrap gap-2">{files.map(file => <Link key={file.id} className="btn-outline" to={`/media/${file.id}`}>{detail?.kind === 'series' ? `第 ${file.episode_num} 集` : '播放电影'} · 文件详情</Link>)}</div>{fileTotal > 50 && <div className="flex gap-2"><button className="btn-outline" disabled={filePage === 1} onClick={() => setFilePage(v => v - 1)}>上一页文件</button><button className="btn-outline" disabled={filePage * 50 >= fileTotal} onClick={() => setFilePage(v => v + 1)}>下一页文件</button></div>}</> : <p className="text-sm text-ink-50">暂无可见本地文件。</p>}</div>
      </div></div>
    </div>
  </ModalShell>
}
