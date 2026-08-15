import { LoaderCircle } from 'lucide-react'

import type { Media, MediaTrack } from '../types'

type MediaDetailTracksProps = {
  media: Media | null
  versions: Media[]
  selectedVersionID: string
  loading: boolean
  probing: boolean
  probeError: string
  onVersionChange: (id: string) => void
}

export function MediaDetailTracks({
  media,
  versions,
  selectedVersionID,
  loading,
  probing,
  probeError,
  onVersionChange,
}: MediaDetailTracksProps) {
  const tracks = media?.tracks ?? []
  const unavailableText = loading
    ? '正在加载媒体信息…'
    : probing
      ? '正在探测媒体信息…'
      : ''

  return (
    <section className="space-y-3" aria-labelledby="media-tracks-heading">
      <h2 id="media-tracks-heading" className="text-xs font-bold uppercase tracking-widest text-brand-500">
        媒体信息
      </h2>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <label className="min-w-0 space-y-1">
          <span className="text-xs font-semibold text-gray-500">版本</span>
          <select
            className="input-base min-w-0 w-full"
            value={selectedVersionID}
            disabled={versions.length === 0}
            onChange={(event) => onVersionChange(event.target.value)}
          >
            {versions.length === 0 ? (
              <option value="">暂无可用版本</option>
            ) : versions.map((version, index) => (
              <option key={version.id} value={version.id}>{mediaVersionLabel(version, index)}</option>
            ))}
          </select>
        </label>

        <TrackSelect type="video" label="视频" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
        <TrackSelect type="audio" label="音频" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
        <TrackSelect type="subtitle" label="字幕" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
      </div>

      {(loading || probing || probeError) && (
        <div className="flex items-center gap-2 text-sm font-semibold text-gray-500" aria-live="polite">
          {(loading || probing) && <LoaderCircle size={16} className="animate-spin text-brand-500" aria-hidden="true" />}
          <span>{unavailableText || probeError}</span>
        </div>
      )}
    </section>
  )
}

function TrackSelect({
  type,
  label,
  mediaID,
  tracks,
  unavailableText,
}: {
  type: MediaTrack['type']
  label: string
  mediaID?: string
  tracks: MediaTrack[]
  unavailableText: string
}) {
  const items = tracks.filter((track) => track.type === type)
  const selected = items.find((track) => track.is_default) ?? items[0]
  const placeholder = unavailableText || `暂无${label}轨道`

  return (
    <label className="min-w-0 space-y-1">
      <span className="text-xs font-semibold text-gray-500">{label}</span>
      <select
        key={`${mediaID ?? 'loading'}-${type}-${items.map((item) => item.index).join(',')}`}
        className="input-base min-w-0 w-full"
        defaultValue={selected ? String(selected.index) : ''}
        disabled={items.length === 0}
      >
        {items.length === 0 ? (
          <option value="">{placeholder}</option>
        ) : items.map((track) => (
          <option key={track.index} value={track.index}>{trackLabel(track)}</option>
        ))}
      </select>
    </label>
  )
}

function mediaVersionLabel(media: Media, index: number): string {
  const facts: string[] = []
  if (media.width > 0 && media.height > 0) facts.push(`${media.width} × ${media.height}`)
  if (media.video_codec) facts.push(media.video_codec.toUpperCase())
  if (media.container) facts.push(media.container.toUpperCase())
  const source = (media.strm_url || media.relative_path || media.path).split(/[?#]/, 1)[0]
  const filename = source.split(/[\\/]/).pop()?.trim()
  if (filename) facts.push(filename)
  return facts.length > 0 ? facts.join(' · ') : `版本 ${index + 1}`
}

function trackLabel(track: MediaTrack): string {
  return [track.display_title || track.title || track.codec || '未知轨道', ...trackFacts(track)].join(' · ')
}

function trackFacts(track: MediaTrack): string[] {
  const facts: string[] = [`#${track.index}`]
  if (track.type === 'video') {
    if (track.width && track.height) facts.push(`${track.width} × ${track.height}`)
    if (track.profile) facts.push(track.profile)
    if (track.bit_depth) facts.push(`${track.bit_depth}-bit`)
    if (track.video_range && track.video_range !== 'SDR') facts.push(track.video_range)
    if (track.average_frame_rate) facts.push(`${formatNumber(track.average_frame_rate)} fps`)
    if (track.bit_rate) facts.push(formatBitRate(track.bit_rate))
  } else if (track.type === 'audio') {
    if (track.display_language) facts.push(track.display_language)
    if (track.channel_layout) facts.push(track.channel_layout)
    else if (track.channels) facts.push(`${track.channels} 声道`)
    if (track.sample_rate) facts.push(`${formatNumber(track.sample_rate / 1000)} kHz`)
    if (track.bit_rate) facts.push(formatBitRate(track.bit_rate))
  } else {
    if (track.display_language) facts.push(track.display_language)
    if (track.is_forced) facts.push('强制')
  }
  if (track.is_default) facts.push('默认')
  return facts
}

function formatBitRate(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)} Mbps`
  return `${Math.round(value / 1000)} kbps`
}

function formatNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}
