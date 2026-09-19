import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { hongguoAPI, type HongGuoDetail, type HongGuoGroup } from '../api/hongguo'
import type { Media } from '../types'

export function HongGuoLibraryDetail({ sourceID, onClose }: { sourceID: string; onClose: () => void }) {
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
    {detail?.kind === 'series' && <HongGuoGrouping key={detail.source_id} detail={detail} navigate={(changes) => {
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

function HongGuoGrouping({ detail, navigate }: { detail: HongGuoDetail; navigate: (changes: Record<string, string>) => void }) {
  const groupID = detail.group?.group_id
  const [group, setGroup] = useState<HongGuoGroup | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!groupID) return
    const controller = new AbortController()
    setGroup(null); setError(false)
    void hongguoAPI.group(groupID, controller.signal).then((value) => {
      if (!controller.signal.aborted) setGroup(value)
    }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [groupID, retry])
  return <section className="space-y-3 border-t border-ink-100/10 pt-4">
    <h3 className="text-lg font-semibold">官方系列剧</h3>
    {!groupID ? <p className="text-sm text-ink-50">暂无官方系列关系，作为独立剧展示。</p> : error ? <p role="alert">系列读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p> : !group ? <p role="status">读取系列中…</p> : <><p>{group.title}</p><div className="flex flex-wrap gap-2">{group.members.map((m) => <button className="btn-outline" key={m.source_id} onClick={() => navigate({ id: m.source_id })}>第 {m.season_number} 季 · {m.title}</button>)}</div></>}
  </section>
}
