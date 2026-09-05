import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { mediaAPI } from '../api/library'
import type { Media } from '../types'
import type { HistoryItem } from '../types/history'
import type { SeriesCard } from '../utils/groupSeries'
import { LibrarySeriesDetailHeader } from './LibrarySeriesDetailHeader'
import { LibrarySeriesEpisodes } from './LibrarySeriesEpisodes'
import { LibrarySeriesEpisodeDetail } from './LibrarySeriesEpisodeDetail'
import { MediaDetailCast } from './MediaDetailCast'
import { episodeIdentity, resolveSeriesSelection } from './seriesDetailModel'

type LibrarySeriesDetailSectionProps = {
  selectedSeries: SeriesCard | null
  selectedEpisodes: { season: number; episodes: Media[] }[]
  allEpisodes: Media[]
  history: HistoryItem[]
  loadingEpisodes: boolean
  episodesError: boolean
  playbackFrom: string
  isAdmin: boolean
  seriesToolBusy: string
  onBack: () => void
  onSmartScrape: () => void
  onMetadataEdit: () => void
  onProbe: () => void
  onOrganize: () => void
  onSoftDelete: () => void
  onSeasonChange: (season: number) => void
  onChanged: () => void
}

export function LibrarySeriesDetailSection({ selectedSeries, selectedEpisodes, allEpisodes, history, loadingEpisodes, episodesError, playbackFrom, isAdmin, seriesToolBusy, onBack, onSmartScrape, onMetadataEdit, onProbe, onOrganize, onSoftDelete, onSeasonChange, onChanged }: LibrarySeriesDetailSectionProps) {
  const [params, setParams] = useSearchParams()
  const { season, episode } = resolveSeriesSelection(selectedEpisodes, params)
  const episodeID = episode ? episodeIdentity(episode) : ''
  const mediaID = episode?.id ?? ''
  const [versionResult, setVersionResult] = useState<{ episodeID: string; items: Media[] } | null>(null)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!mediaID || !episodeID || loadingEpisodes) return
    let cancelled = false
    mediaAPI.listVersions(mediaID).then((items) => {
      if (!cancelled) setVersionResult({ episodeID, items: items.filter((item) => episodeIdentity(item) === episodeID) })
    }).catch(() => { if (!cancelled) setVersionResult({ episodeID, items: [] }) })
    return () => { cancelled = true }
  }, [mediaID, episodeID, loadingEpisodes, retry])
  const versions = versionResult?.episodeID === episodeID && !loadingEpisodes ? versionResult.items : []
  const version = versions.find((item) => item.id === params.get('version')) ?? versions.find((item) => item.id === mediaID) ?? versions[0]
  const versionID = version?.id
  useEffect(() => {
    if (!selectedSeries || !season || !episodeID || loadingEpisodes) return
    const next = new URLSearchParams(params)
    if (params.get('series_id')) {
      next.set('series_id', params.get('series_id')!)
      next.delete('series')
    } else next.set('series', selectedSeries.key)
    next.set('season', String(season.season))
    next.set('episode', episodeID)
    if (versionID) next.set('version', versionID)
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [selectedSeries, season, episodeID, versionID, params, setParams, loadingEpisodes])

  if (!selectedSeries) return null
  const selectEpisode = (media: Media) => {
    const next = new URLSearchParams(params)
    next.set('season', String(season.season))
    next.set('episode', episodeIdentity(media))
    next.delete('version')
    setParams(next)
  }
  const selectVersion = (id: string) => {
    if (!versions.some((item) => item.id === id)) return
    const next = new URLSearchParams(params)
    next.set('version', id)
    setParams(next)
  }
  return (
    <div className="min-w-0 space-y-8">
      <LibrarySeriesDetailHeader series={selectedSeries} allEpisodes={loadingEpisodes ? [] : allEpisodes} history={history} playbackFrom={playbackFrom} isAdmin={isAdmin} seriesToolBusy={seriesToolBusy} onBack={onBack} onSmartScrape={onSmartScrape} onMetadataEdit={onMetadataEdit} onProbe={onProbe} onOrganize={onOrganize} onSoftDelete={onSoftDelete} />
      {episodesError ? <div role="status" className="flex flex-wrap items-center gap-3 text-[var(--app-muted)]">分集加载失败<button className="btn-outline" onClick={onChanged}>重试分集</button></div> : <LibrarySeriesEpisodes loading={loadingEpisodes} selectedEpisodes={selectedEpisodes} selectedSeason={season?.season ?? 1} visibleEpisodes={season?.episodes ?? []} selectedEpisodeID={episodeID} selectedVersionID={versionID} history={history} playbackFrom={playbackFrom} onSeasonChange={onSeasonChange} onEpisodeSelect={selectEpisode} />}
      {episode && !loadingEpisodes && (
        <section aria-label="当前单集" className="relative space-y-5 rounded-3xl border border-[var(--app-border)] bg-[var(--app-panel)] p-5 sm:p-8">
          <p className="text-sm font-bold tracking-wider text-brand-500">当前单集 · S{season.season} E{episode.episode_num}</p>
          {version ? <LibrarySeriesEpisodeDetail key={version.id} mediaID={version.id} episodeID={episodeID} versions={versions} onVersionChange={selectVersion} playbackFrom={playbackFrom} isAdmin={isAdmin} onChanged={onChanged} /> : (
            <p role="status" className="text-sm text-[var(--app-muted)]">
              {versionResult?.episodeID === episodeID ? '当前单集没有可用版本或加载失败。' : '正在加载当前单集版本…'}
              {versionResult?.episodeID === episodeID && <button type="button" className="btn-outline ml-3" onClick={() => { setVersionResult(null); setRetry((value) => value + 1) }}>重试</button>}
            </p>
          )}
        </section>
      )}
      <MediaDetailCast key={selectedSeries.rep.id} mediaId={selectedSeries.rep.id} scope="series" />
    </div>
  )
}
