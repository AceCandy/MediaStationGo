import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import toast from 'react-hot-toast'
import { hongguoAPI, type HongGuoDetail, type HongGuoGroup, type HongGuoGroupInput } from '../api/hongguo'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useAuthStore } from '../stores/auth'
import type { Media } from '../types'

export function HongGuoLibraryDetail({ sourceID, onClose }: { sourceID: string; onClose: () => void }) {
  const admin = useAuthStore((s) => s.user?.role === 'admin')
  const [params, setParams] = useSearchParams()
  const [detail, setDetail] = useState<HongGuoDetail | null>(null)
  const [error, setError] = useState(false)
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void hongguoAPI.detail(sourceID, controller.signal).then((data) => { if (!controller.signal.aborted) setDetail(data) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [sourceID, revision])
  return <section className="space-y-5">
    <button className="btn-outline" onClick={onClose}>返回媒体库</button>
    {error && <p role="alert">资料读取失败 <button className="btn-outline" onClick={() => setRevision((v) => v + 1)}>重试</button></p>}
    <h2 className="text-2xl font-bold">{detail?.title || '红果短剧'}</h2>
    <HongGuoFiles sourceID={sourceID} />
    {detail?.kind === 'series' && <HongGuoGrouping key={revision} detail={detail} admin={admin} onSaved={() => setRevision((v) => v + 1)} navigate={(changes) => {
      const next = new URLSearchParams(params); next.set('hongguo_id', changes.id); next.delete('media_page'); setParams(next)
    }} />}
  </section>
}

function HongGuoFiles({ sourceID }: { sourceID: string }) {
  const [params, setParams] = useSearchParams()
  const rawPage = Number(params.get('media_page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage > 0 && rawPage <= 1000000 ? rawPage : 1
  const [result, setResult] = useState<{ page: number; items: Media[]; total: number } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (rawPage === page && params.getAll('media_page').length <= 1) return
    const next = new URLSearchParams(params); next.set('media_page', String(page)); setParams(next, { replace: true })
  }, [params, setParams, page, rawPage])
  useEffect(() => {
    const controller = new AbortController()
    setError(false)
    void hongguoAPI.media(sourceID, page, controller.signal).then((data) => {
      if (!controller.signal.aborted) setResult({ ...data, page })
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [sourceID, page, retry])
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('media_page', String(value)); setParams(next) }
  return <section className="space-y-3"><h3 className="text-lg font-semibold">本地媒体与 STRM</h3>
    {error ? <p role="alert">媒体读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : result?.page !== page ? <p role="status">加载媒体中…</p> : <>
      {result.items.length === 0 ? <p className="text-sm text-ink-50">暂无可访问的已匹配媒体。导入资料并扫描红果短剧类型媒体库后会显示在这里。</p> : <div className="grid gap-2 sm:grid-cols-2">{result.items.map((media) => <Link className="card min-w-0 p-3" key={media.id} to={`/play/${encodeURIComponent(media.id)}`} state={{ from: `${location.pathname}?${params.toString()}` }}><span className="font-semibold">播放 · {media.episode_num > 0 ? `S${media.season_num}E${String(media.episode_num).padStart(3, '0')}` : media.title}</span><p className="break-all text-xs text-ink-50">{media.relative_path || media.title}</p></Link>)}</div>}
      {result.total > 50 && <div className="flex flex-wrap items-center gap-2"><button className="btn-outline" disabled={page <= 1} onClick={() => goPage(page - 1)}>上一页媒体</button><span>第 {page} 页 · 共 {result.total} 个文件</span><button className="btn-outline" disabled={page * 50 >= result.total} onClick={() => goPage(page + 1)}>下一页媒体</button></div>}
    </>}
  </section>
}

function HongGuoGrouping({ detail, admin, navigate, onSaved }: { detail: HongGuoDetail; admin: boolean; navigate: (changes: Record<string, string>) => void; onSaved: () => void }) {
  const groupID = detail.group?.group_id
  const [group, setGroup] = useState<HongGuoGroup | null>(null)
  const [loading, setLoading] = useState(Boolean(groupID))
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(detail.title)
  const [members, setMembers] = useState<HongGuoGroupInput[]>([{ source_id: detail.source_id, season_number: 1 }])
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  useEffect(() => {
    if (!groupID) return
    const controller = new AbortController()
    setLoading(true); setError(false)
    void hongguoAPI.group(groupID, controller.signal).then((value) => {
      if (controller.signal.aborted) return
      setGroup(value); setTitle(value.title); setMembers(value.members.map((m) => ({ source_id: m.source_id, season_number: m.season_number })))
    }).catch(() => { if (!controller.signal.aborted) setError(true) }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [groupID, retry])
  const save = async () => {
    if (busy) return
    if (!title.trim() || !members.length || members.some((m) => !/^[1-9][0-9]{0,31}$/.test(m.source_id) || !Number.isInteger(m.season_number) || m.season_number < 1 || m.season_number > 1000) || new Set(members.map((m) => m.source_id)).size !== members.length || new Set(members.map((m) => m.season_number)).size !== members.length) {
      toast.error('请检查作品 ID 和季号，不能重复'); return
    }
    setBusy(true)
    try { await hongguoAPI.saveGroup(groupID, title.trim(), members); toast.success('聚合已保存'); onSaved() }
    catch { toast.error('保存失败：请先导入源作品，并确认不是电影或其他聚合的成员') }
    finally { setBusy(false) }
  }
  return <section className="space-y-3 border-t border-ink-100/10 pt-4">
    <h3 className="text-lg font-semibold">人工跨季聚合</h3>
    {loading ? <p role="status">读取聚合中…</p> : error ? <p role="alert">聚合读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : <>
      {group ? <><p>{group.title}</p><div className="flex flex-wrap gap-2">{group.members.map((m) => <button className="btn-outline" key={m.source_id} onClick={() => navigate({ id: m.source_id })}>第 {m.season_number} 季 · {m.title}</button>)}</div></> : <p className="text-sm text-ink-50">未聚合，作为独立剧的第一季展示。</p>}
      {admin && !editing && <button className="btn-outline" onClick={() => setEditing(true)}>{group ? '编辑聚合' : '建立聚合'}</button>}
      {admin && editing && <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void save() }}>
        <label className="block">聚合剧名<input className="input-field mt-1 w-full" required value={title} disabled={busy} onChange={(e) => setTitle(e.target.value)} /></label>
        {members.map((member, index) => <div className="flex flex-wrap items-end gap-2" key={index}>
          <label className="min-w-0 flex-1">源作品 ID<input className="input-field mt-1 w-full" required disabled={busy} value={member.source_id} onChange={(e) => setMembers((rows) => rows.map((m, i) => i === index ? { ...m, source_id: e.target.value.trim() } : m))} /></label>
          <label>呈现季号<input className="input-field mt-1 block w-24" type="number" min={1} max={1000} required disabled={busy} value={member.season_number} onChange={(e) => setMembers((rows) => rows.map((m, i) => i === index ? { ...m, season_number: Number(e.target.value) } : m))} /></label>
          <button className="btn-outline" type="button" disabled={busy || members.length === 1} onClick={() => setMembers((rows) => rows.filter((_, i) => i !== index))}>移除</button>
        </div>)}
        <p className="text-sm text-ink-50">文件仍使用各源作品 ID 与 S01Exxx；聚合不改文件，不改变观看进度。</p>
        <div className="flex flex-wrap gap-2"><button className="btn-outline" type="button" disabled={busy || members.length >= 1000} onClick={() => setMembers((rows) => [...rows, { source_id: '', season_number: Math.max(...rows.map((m) => m.season_number)) + 1 }])}>添加一季</button><button className="btn-primary" disabled={busy}>保存聚合</button><button className="btn-outline" type="button" disabled={busy} onClick={() => setEditing(false)}>取消编辑</button>{groupID && <button className="btn-danger" type="button" disabled={busy} onClick={() => setConfirmDelete(true)}>解除整个聚合</button>}</div>
      </form>}
    </>}
    {confirmDelete && groupID && <ConfirmDialog options={{ title: '解除聚合', message: '所有成员将恢复独立剧展示，资料、文件和观看进度均保留。', confirmText: '解除聚合' }} onClose={(confirmed) => {
      setConfirmDelete(false)
      if (!confirmed || busy) return
      setBusy(true)
      void hongguoAPI.deleteGroup(groupID).then(() => { toast.success('已解除聚合'); onSaved() }).catch(() => toast.error('解除失败')).finally(() => setBusy(false))
    }} />}
  </section>
}
