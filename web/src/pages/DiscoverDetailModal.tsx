import { useEffect, useState } from 'react'
import { discoverAPI, discoverTMDbIdentity, type DiscoverItem } from '../api/discover'
import { ModalShell } from '../components/ModalShell'
import { TMDbDetailModal } from './TMDbDetailModal'
import { MetadataOverview } from './MediaDetailMetadata'
import { discoverItemSource } from './discoverPageModel'
import { doubanAPI, type DoubanSubjectSummary } from '../api/douban'
import { DoubanSubjectDetails } from './DoubanDataCenterPage'
import {
  DiscoverArtworkPanel,
  DiscoverModalHeader,
} from './DiscoverDetailModalSections'

export function DiscoverDetailModal({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  return discoverItemSource(item) === 'douban' && item.douban_id
    ? <DoubanRankingDetail key={item.douban_id} item={item} onClose={onClose} />
    : <DiscoverSourceDetail item={item} onClose={onClose} />
}

function DoubanRankingDetail({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  const [subject, setSubject] = useState<DoubanSubjectSummary | null>(null)
  const [failed, setFailed] = useState(false)
  const [retry, setRetry] = useState(0)
  const kind = item.media_type === 'tv' ? 'series' : item.media_type === 'movie' ? 'movie' : ''
  useEffect(() => {
    const controller = new AbortController()
    setFailed(false)
    doubanAPI.ensure(item.douban_id!, kind, controller.signal).then(value => {
      if (!controller.signal.aborted) setSubject(value)
    }).catch(() => { if (!controller.signal.aborted) setFailed(true) })
    return () => controller.abort()
  }, [item.douban_id, kind, retry])
  return subject ? <DoubanSubjectDetails subject={subject} onClose={onClose} /> : <ModalShell onClose={onClose} maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto p-5 sm:p-6" ariaLabel={item.title}>
    <DiscoverModalHeader item={item} source="douban" onClose={onClose} />
    <div className="grid gap-5 lg:grid-cols-[260px_1fr]"><DiscoverArtworkPanel item={item} /><div><MetadataOverview overview={item.overview || '正在读取来源资料…'} /><p role={failed ? 'alert' : 'status'} className="mt-4 text-sm text-ink-100">{failed ? '豆瓣资料读取或补全请求失败，请稍后重试。' : '正在读取豆瓣资料并登记缺项补全…'}</p>{failed && <button className="btn-outline mt-3" onClick={() => setRetry(value => value + 1)}>重试详情</button>}</div></div>
  </ModalShell>
}

function DiscoverSourceDetail({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  const identity = discoverTMDbIdentity(item)
  const [centerID, setCenterID] = useState('')
  const [error, setError] = useState(false)
  const [revision, setRevision] = useState(0)
  const tmdbID = identity?.tmdb_id, mediaType = identity?.media_type
  useEffect(() => {
    if (!tmdbID || !mediaType) return
    const c = new AbortController(); setError(false)
    discoverAPI.detail({tmdb_id:tmdbID, media_type:mediaType}, c.signal).then(value => {
      if (!c.signal.aborted) { if (value.center_id) setCenterID(value.center_id); else setError(true) }
    }).catch(() => {if (!c.signal.aborted) setError(true)})
    return () => c.abort()
  }, [tmdbID, mediaType, revision])
  if (centerID) return <TMDbDetailModal initial={{id:centerID, person:false, title:item.title, image:item.poster_url, overview:item.overview}} onClose={onClose} />
  return <ModalShell onClose={onClose} maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto p-5 sm:p-6" ariaLabel={item.title}>
    <DiscoverModalHeader item={item} source={discoverItemSource(item)} onClose={onClose} />
    <div className="grid gap-5 lg:grid-cols-[260px_1fr]"><DiscoverArtworkPanel item={item} /><div className="space-y-4"><MetadataOverview overview={item.overview || '当前数据源没有返回简介。'} />{identity && <p role={error ? 'alert' : 'status'}>{error ? '完整资料暂时不可用' : '正在加载完整资料…'}{error && <button className="btn-outline ml-2" onClick={() => setRevision(v => v + 1)}>重试详情</button>}</p>}</div></div>
  </ModalShell>
}
