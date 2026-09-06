import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Play } from 'lucide-react'
import toast from 'react-hot-toast'

import { mediaAPI } from '../api/library'
import { playbackAPI } from '../api/playback'
import type { Media } from '../types'
import type { HistoryItem } from '../types/history'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'
import { MediaDetailBackdrop, MediaDetailPoster } from './MediaDetailArtwork'
import { MediaDetailMetadata } from './MediaDetailMetadata'
import { MediaDetailAdminMenu } from './MediaDetailAdminPanel'
import { episodeLabel, seriesResumeEpisode } from './seriesDetailModel'

type LibrarySeriesDetailHeaderProps = {
  series: SeriesCard
  allEpisodes: Media[]
  history: HistoryItem[]
  playbackFrom: string
  isAdmin: boolean
  seriesToolBusy: string
  onBack: () => void
  onMetadataEdit: () => void
  onProbe: () => void
  onSoftDelete: () => void
}

export function LibrarySeriesDetailHeader({ series, allEpisodes, history, playbackFrom, isAdmin, seriesToolBusy, onBack, onMetadataEdit, onProbe, onSoftDelete }: LibrarySeriesDetailHeaderProps) {
  const [data, setData] = useState<{ series: Media; favourite: boolean } | null>(null)
  const [failed, setFailed] = useState(false)
  const [revision, setRevision] = useState(0)
  const favouritePending = useRef(false)
  useEffect(() => {
    let cancelled = false
    setData(null)
    setFailed(false)
    mediaAPI.series(series.rep.id)
      .then((result) => { if (!cancelled) setData(result) })
      .catch(() => { if (!cancelled) setFailed(true) })
    return () => { cancelled = true }
  }, [series.rep.id, revision])

  const toggleFavourite = async () => {
    if (!data || favouritePending.current) return
    favouritePending.current = true
    try {
      const favourite = await playbackAPI.setSeriesFavourite(series.rep.id, !data.favourite)
      setData((current) => current ? { ...current, favourite } : current)
      toast.success(favourite ? '已收藏整剧' : '已取消整剧收藏')
    } catch { toast.error('整剧收藏操作失败') }
    finally { favouritePending.current = false }
  }
  const resume = seriesResumeEpisode(allEpisodes, history)
  const resumeHistory = history.find((row) => row.metadata_id === resume?.metadata_id)
  const continuing = resumeHistory && !resumeHistory.completed && resumeHistory.position_ms >= 20_000
  const resumeFrom = () => {
    const url = new URL(playbackFrom, window.location.origin)
    if (resume) {
      url.searchParams.set('season', String(resume.season_num))
      url.searchParams.set('episode', resume.metadata_id || resume.id)
      url.searchParams.set('version', resume.id)
    }
    return url.pathname + url.search
  }
  const actions = <div className="space-y-3">
    <p className="text-sm text-[var(--app-muted)]">共 {series.count} {allEpisodes.some((ep) => ep.episode_num <= 0) ? '项' : '集'}</p>
    <div className="flex flex-wrap items-center gap-3">
      {resume && <Link to={`/play/${resume.id}`} state={{ from: resumeFrom() }} className="btn-primary"><Play size={16} fill="currentColor" aria-hidden="true" />{continuing ? '继续观看' : '播放'} · {resume.episode_num > 0 ? `S${resume.season_num} E${resume.episode_num}` : episodeLabel(resume)}</Link>}
      {isAdmin && allEpisodes.length > 0 && <MediaDetailAdminMenu label="整剧更多操作" disabled={!!seriesToolBusy} tmdbRefreshPending={false} doubanEnrichmentPending={false} doubanDegraded={false} onMetadataEdit={onMetadataEdit} onProbe={onProbe} onSoftDelete={onSoftDelete} />}
    </div>
  </div>

  return (
    <div className="relative isolate rounded-3xl border border-[var(--app-border)] bg-[var(--app-panel)]">
      <div className="pointer-events-none absolute inset-0 -z-10 overflow-hidden rounded-3xl">{data && <MediaDetailBackdrop media={data.series} />}</div>
      <div className="p-5 sm:p-8">
        <button type="button" onClick={onBack} className="btn-ghost gap-2"><ArrowLeft size={16} />返回媒体库</button>
        <div className="mt-5 flex items-start gap-4 sm:gap-8 lg:gap-12">
          {data && <div className="w-20 shrink-0 sm:w-36 md:w-48 lg:w-56"><MediaDetailPoster media={data.series} playable={false} /></div>}
          <div className="min-w-0 flex-1 space-y-4">
            {data ? <MediaDetailMetadata media={data.series} scope="series" isAdmin={isAdmin} favourite={data.favourite} onToggleFavourite={toggleFavourite} onMetadataEdit={onMetadataEdit} actions={actions} /> : (
              <><h1 className="font-display text-3xl font-bold text-[var(--app-text)]">{seriesTitle(series.rep)}</h1><p role="status" className="text-sm text-[var(--app-muted)]">{failed ? '整剧信息暂不可用，仍可在下方选集。' : '正在加载整剧信息…'}</p>{failed && <button className="btn-outline" onClick={() => setRevision((value) => value + 1)}>重试整剧信息</button>}</>
            )}
            {!data && actions}
          </div>
        </div>
      </div>
    </div>
  )
}
