import { Link, useNavigate } from 'react-router-dom'
import { Play } from 'lucide-react'

import { imageURL } from '../api/client'
import { ExternalPlayerButton } from '../components/ExternalPlayerButton'
import { ModalShell } from '../components/ModalShell'
import type { Media } from '../types'
import { MediaDetailAdminMenu } from './MediaDetailAdminPanel'
import { MediaDetailMetadata } from './MediaDetailMetadata'
import { MediaDetailTracks } from './MediaDetailTracks'
import { MediaDetailDialogs } from './MediaDetailPageSections'
import { useMediaDetailPageState } from './useMediaDetailPageState'
import { episodeIdentity, episodeLabel } from './seriesDetailModel'

type Props = {
  mediaID: string
  episodeID: string
  versions: Media[]
  playbackFrom: string
  isAdmin: boolean
  onVersionChange: (id: string) => void
  onChanged: () => void
}

// 按具体文件挂载，切版本时销毁旧请求和弹窗；单集管理与播放使用同一身份。
export function LibrarySeriesEpisodeDetail({ mediaID, episodeID, versions, playbackFrom, isAdmin, onVersionChange, onChanged }: Props) {
  const navigate = useNavigate()
  const detail = useMediaDetailPageState({ id: mediaID, navigate, backTarget: playbackFrom, singleVersion: true })
  const media = detail.media?.id === mediaID && episodeIdentity(detail.media) === episodeID ? detail.media : null
  const tracks = <MediaDetailTracks media={media} versions={versions} selectedVersionID={mediaID} loading={detail.loading} probing={detail.probing} probeError={detail.probeError} onVersionChange={onVersionChange} readOnlyTracks />
  return (
    <>
      {detail.tmdbRefreshPending && <ModalShell ariaLabel="正在刷新单集 TMDB 信息" maxWidth="max-w-sm" zIndex={100}><p role="status" className="p-8 text-center text-[var(--app-text)]">正在刷新单集信息，请稍候…</p></ModalShell>}
      <div className="grid min-w-0 items-start gap-6 md:grid-cols-[18rem_minmax(0,1fr)] md:grid-rows-[auto_1fr] lg:grid-cols-[22rem_minmax(0,1fr)] lg:gap-x-10">
        {media && <div className="flex aspect-video items-center justify-center overflow-hidden rounded-2xl bg-[var(--app-hover)] text-sm text-[var(--app-muted)] md:col-start-1 md:row-start-1">
          {media.backdrop_url ? <img src={imageURL(media.backdrop_url, media.updated_at)} alt={`${media.title} 剧照`} width={640} height={360} loading="lazy" className="h-full w-full object-cover" referrerPolicy="no-referrer" /> : <span>{episodeLabel(media)} · 暂无剧照</span>}
        </div>}
        <div className="min-w-0 space-y-5 md:col-start-2 md:row-span-2 md:row-start-1">
          {media ? <MediaDetailMetadata key={media.id} media={media} selectedMedia={media} scope="episode" isAdmin={isAdmin} onMetadataEdit={() => detail.setMetadataEditOpen(true)} actions={
            <div className="flex flex-wrap items-center gap-3">
              <Link to={`/play/${media.id}`} state={{ from: playbackFrom }} className="btn-primary"><Play size={16} fill="currentColor" aria-hidden="true" />{media.episode_num > 0 ? '播放此集' : '播放文件'}</Link>
              <ExternalPlayerButton mediaId={media.id} />
              {isAdmin && <MediaDetailAdminMenu label="当前单集 / 文件操作" onTMDbRefresh={detail.refreshTMDb} tmdbRefreshPending={detail.tmdbRefreshPending} doubanEnrichmentPending={false} doubanDegraded={false} onMetadataEdit={() => detail.setMetadataEditOpen(true)} onProbe={detail.reprobe} onSoftDelete={async () => { if (await detail.softDelete()) onChanged() }} />}
            </div>
          } /> : <p role="status" className="text-sm text-[var(--app-muted)]">{detail.loading ? '正在加载当前版本…' : '当前版本不可用'}{!detail.loading && <button className="btn-outline ml-3" onClick={() => void detail.refresh().catch(() => undefined)}>重试</button>}</p>}
        </div>
        <div className="min-w-0 md:col-start-1 md:row-start-2">{tracks}</div>
      </div>
      {media && <MediaDetailDialogs media={media} metadataEditOpen={detail.metadataEditOpen} onMetadataEditClose={() => detail.setMetadataEditOpen(false)} onMetadataSaved={detail.handleMetadataSaved} />}
    </>
  )
}
