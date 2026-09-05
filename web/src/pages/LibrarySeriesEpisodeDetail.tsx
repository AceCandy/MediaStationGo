import { Link, useNavigate } from 'react-router-dom'
import { Play } from 'lucide-react'

import { ExternalPlayerButton } from '../components/ExternalPlayerButton'
import { ModalShell } from '../components/ModalShell'
import type { Media } from '../types'
import { MediaDetailAdminMenu } from './MediaDetailAdminPanel'
import { MediaDetailMetadata } from './MediaDetailMetadata'
import { MediaDetailTracks } from './MediaDetailTracks'
import { MediaDetailDialogs } from './MediaDetailPageSections'
import { useMediaDetailPageState } from './useMediaDetailPageState'
import { episodeIdentity } from './seriesDetailModel'

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
  return (
    <>
      {detail.tmdbRefreshPending && <ModalShell ariaLabel="正在刷新单集 TMDB 信息" maxWidth="max-w-sm" zIndex={100}><p role="status" className="p-8 text-center text-[var(--app-text)]">正在刷新单集信息，请稍候…</p></ModalShell>}
      <div className="grid min-w-0 gap-7 md:grid-cols-[16rem_minmax(0,1fr)]">
        <MediaDetailTracks media={media} versions={versions} selectedVersionID={mediaID} loading={detail.loading} probing={detail.probing} probeError={detail.probeError} onVersionChange={onVersionChange} />
        <div className="min-w-0 space-y-5">
          {media ? <>
            <MediaDetailMetadata key={media.id} media={media} selectedMedia={media} scope="episode" isAdmin={isAdmin} onMetadataEdit={() => detail.setMetadataEditOpen(true)} />
            <div className="flex flex-wrap items-center gap-3">
              <Link to={`/play/${media.id}`} state={{ from: playbackFrom }} className="btn-primary"><Play size={16} fill="currentColor" />播放当前版本</Link>
              <ExternalPlayerButton mediaId={media.id} />
              {isAdmin && <MediaDetailAdminMenu label="当前单集 / 文件操作" onTMDbRefresh={detail.refreshTMDb} tmdbRefreshPending={detail.tmdbRefreshPending} doubanEnrichmentPending={false} doubanDegraded={false} onMetadataEdit={() => detail.setMetadataEditOpen(true)} onOrganize={() => detail.setOrganizeOpen(true)} onProbe={detail.reprobe} onSoftDelete={async () => { if (await detail.softDelete()) onChanged() }} />}
            </div>
            <p className="text-xs text-[var(--app-muted)]">版本决定播放文件；视频、音频和字幕选项用于查看轨道信息。</p>
          </> : <p role="status" className="text-sm text-[var(--app-muted)]">{detail.loading ? '正在加载当前版本…' : '当前版本不可用'}{!detail.loading && <button className="btn-outline ml-3" onClick={() => void detail.refresh().catch(() => undefined)}>重试</button>}</p>}
        </div>
      </div>
      {media && <MediaDetailDialogs media={media} metadataEditOpen={detail.metadataEditOpen} organizeOpen={detail.organizeOpen} onMetadataEditClose={() => detail.setMetadataEditOpen(false)} onOrganizeClose={() => detail.setOrganizeOpen(false)} onMetadataSaved={detail.handleMetadataSaved} onOrganized={onChanged} />}
    </>
  )
}
