import { useCallback, useEffect, useRef, useState } from 'react'
import { discoverAPI, discoverTMDbIdentity, type DiscoverDetail, type DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'
import { useAuthStore } from '../stores/auth'
import { MediaCredits } from './MediaDetailCast'
import { MetadataFacts, MetadataOverview, MetadataCategories, ProviderBadge } from './MediaDetailMetadata'
import { discoverItemSource } from './discoverPageModel'
import {
  DiscoverArtworkPanel,
  DiscoverModalHeader,
} from './DiscoverDetailModalSections'

export function DiscoverDetailModal({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  const source = discoverItemSource(item)
  const identity = discoverTMDbIdentity(item)
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin')
  const [detail, setDetail] = useState<DiscoverDetail | null>(null)
  const [error, setError] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshError, setRefreshError] = useState(false)
  const [revision, setRevision] = useState(0)
  const controllerRef = useRef<AbortController | null>(null)
  const tmdbID = identity?.tmdb_id
  const mediaType = identity?.media_type
  const loadDetail = useCallback((keepCurrent: boolean) => {
    if (!tmdbID || !mediaType) return null
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    if (!keepCurrent) setDetail(null)
    setError(false)
    setRefreshError(false)
    setRefreshing(false)
    void discoverAPI.detail({ tmdb_id: tmdbID, media_type: mediaType }, controller.signal)
      .then((data) => {
        if (controller.signal.aborted) return
        setDetail(data)
        if (!isAdmin || !data.local_metadata) return
        setRefreshing(true)
        return discoverAPI.refresh({ tmdb_id: tmdbID, media_type: mediaType }, controller.signal)
          .then((latest) => { if (!controller.signal.aborted) setDetail(latest) })
          .catch(() => { if (!controller.signal.aborted) setRefreshError(true) })
          .finally(() => { if (!controller.signal.aborted) setRefreshing(false) })
      })
      .catch(() => { if (!controller.signal.aborted) setError(true) })
    return controller
  }, [isAdmin, mediaType, tmdbID])
  useEffect(() => {
    loadDetail(false)
    return () => {
      controllerRef.current?.abort()
      controllerRef.current = null
    }
  }, [loadDetail, revision])
  const display: DiscoverItem = detail ? { ...item, title: detail.title, overview: detail.overview, poster_url: detail.poster_url, backdrop_url: detail.backdrop_url, rating: detail.rating, year: detail.year } : item
  const doubanID = detail ? detail.douban_id : item.douban_id

  return (
    <ModalShell onClose={onClose} maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto" ariaLabel={display.title}>
      <div className="relative isolate min-h-full p-5 sm:p-6">
        {display.backdrop_url && <div aria-hidden="true" data-discover-backdrop className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">
          <img src={imageURL(display.backdrop_url)} alt="" className="absolute inset-x-0 top-0 h-[min(36rem,100%)] w-full object-cover opacity-40" />
          <div className="absolute inset-0" style={{ background: 'linear-gradient(to bottom, color-mix(in srgb, var(--app-panel) 35%, transparent), var(--app-panel) min(34rem, 75%))' }} />
        </div>}
      <DiscoverModalHeader item={display} source={source} onClose={onClose} />
      <div className="grid gap-5 lg:grid-cols-[260px_1fr]">
        <DiscoverArtworkPanel item={display} />
        <div className="min-w-0 space-y-5">
          {identity && <>
            {detail && <MetadataFacts rating={detail.rating ?? 0} date={detail.release_date || (detail.year ? `${detail.year} 年` : '暂无')} durationSeconds={detail.runtime_minutes.map((minutes) => minutes * 60)} dateLabel={mediaType === 'tv' ? '首播日期' : '上映日期'} runtimeLabel={mediaType === 'tv' ? '单集时长' : '时长'} />}
            <div className="flex flex-wrap gap-2.5">
              <ProviderBadge href={`https://www.themoviedb.org/${identity.media_type}/${identity.tmdb_id}`} label="TMDb" iconSrc="/brand/tmdb.svg" status={detail?.tmdb_status} />
              <ProviderBadge href={doubanID && /^[1-9]\d{0,19}$/.test(doubanID) ? `https://movie.douban.com/subject/${doubanID}/` : undefined} label="豆瓣" iconSrc="/brand/douban.svg" status={doubanID ? detail?.douban_status : 'unlinked'} />
            </div>
            {!detail && !error && <p role="status" className="text-sm text-[var(--app-muted)]">正在加载完整资料…</p>}
            {error && <div role="alert" className="flex flex-wrap items-center gap-3 text-sm"><span>完整资料暂时不可用</span><button className="btn-outline" onClick={() => setRevision((v) => v + 1)}>重试详情</button></div>}
            {detail?.local_metadata && isAdmin && refreshing && <p role="status" className="text-sm text-[var(--app-muted)]">正在刷新 TMDb 最新资料…</p>}
            {detail?.local_metadata && isAdmin && refreshError && <div role="alert" className="flex flex-wrap items-center gap-3 text-sm"><span>TMDb 刷新失败，已保留当前资料</span><button className="btn-outline" onClick={() => loadDetail(true)}>重试刷新</button></div>}
          </>}
          <MetadataOverview overview={display.overview || '当前数据源没有返回简介。'} />
          {detail && <MetadataCategories genres={detail.genres ?? []} countries={detail.countries ?? []} languages={detail.languages ?? []} />}
          {detail && (detail.credits.length ? <MediaCredits credits={detail.credits} /> : <p className="text-sm text-[var(--app-muted)]">暂无演职员资料</p>)}
        </div>
      </div>
      </div>
    </ModalShell>
  )
}
