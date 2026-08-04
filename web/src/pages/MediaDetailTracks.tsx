import { Captions, Video, Volume2 } from 'lucide-react'

import type { MediaTrack } from '../types'

type MediaDetailTracksProps = {
  tracks?: MediaTrack[]
}

export function MediaDetailTracks({ tracks }: MediaDetailTracksProps) {
  if (!tracks || tracks.length === 0) return null

  const groups: Array<{ type: MediaTrack['type']; label: string; icon: typeof Video }> = [
    { type: 'video', label: '视频', icon: Video },
    { type: 'audio', label: '音频', icon: Volume2 },
    { type: 'subtitle', label: '字幕', icon: Captions },
  ]

  return (
    <section className="space-y-4" aria-labelledby="media-tracks-heading">
      <div className="flex items-center justify-between gap-3">
        <h2 id="media-tracks-heading" className="text-xs font-bold uppercase tracking-widest text-brand-500">
          媒体轨道
        </h2>
        <span className="text-xs font-semibold text-gray-400">{tracks.length} 条</span>
      </div>

      <div className="space-y-5">
        {groups.map(({ type, label, icon: Icon }) => {
          const items = tracks.filter((track) => track.type === type)
          if (items.length === 0) return null
          return (
            <div key={type}>
              <div className="mb-2 flex items-center gap-2 text-sm font-bold text-gray-700">
                <Icon size={16} className="text-brand-500" />
                <span>{label}</span>
              </div>
              <div className="divide-y divide-gray-100 border-y border-gray-100">
                {items.map((track) => <TrackRow key={`${track.type}-${track.index}`} track={track} />)}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}

function TrackRow({ track }: { track: MediaTrack }) {
  const facts = trackFacts(track)
  return (
    <div className="grid gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] sm:items-center">
      <div className="min-w-0">
        <p className="break-words text-sm font-semibold text-gray-800">
          {track.display_title || track.title || track.codec || '未知轨道'}
        </p>
        {track.title && track.title !== track.display_title && (
          <p className="mt-1 break-words text-xs font-medium text-gray-400">{track.title}</p>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5 sm:justify-end">
        {facts.map((fact) => (
          <span key={fact} className="rounded-md bg-gray-50 px-2 py-1 text-2xs font-semibold text-gray-500">
            {fact}
          </span>
        ))}
      </div>
    </div>
  )
}

function trackFacts(track: MediaTrack): string[] {
  const facts: string[] = [`#${track.index}`]
  if (track.type === 'video') {
    if (track.width && track.height) facts.push(`${track.width} × ${track.height}`)
    if (track.profile) facts.push(track.profile)
    if (track.bit_depth) facts.push(`${track.bit_depth}-bit`)
    if (track.video_range && track.video_range !== 'SDR') facts.push(track.video_range)
    if (track.pixel_format) facts.push(track.pixel_format)
    if (track.average_frame_rate) facts.push(`${formatNumber(track.average_frame_rate)} fps`)
    if (track.bit_rate) facts.push(formatBitRate(track.bit_rate))
  } else if (track.type === 'audio') {
    if (track.display_language) facts.push(track.display_language)
    if (track.profile) facts.push(track.profile)
    if (track.channel_layout) facts.push(track.channel_layout)
    else if (track.channels) facts.push(`${track.channels} 声道`)
    if (track.sample_rate) facts.push(`${formatNumber(track.sample_rate / 1000)} kHz`)
    if (track.sample_format) facts.push(track.sample_format)
    if (track.bits_per_sample) facts.push(`${track.bits_per_sample}-bit`)
    if (track.bit_rate) facts.push(formatBitRate(track.bit_rate))
  } else {
    if (track.display_language) facts.push(track.display_language)
    facts.push(track.is_text_subtitle ? '文本字幕' : '图形字幕')
    if (track.is_forced) facts.push('强制')
  }
  if (track.is_default) facts.push('默认')
  if (track.is_hearing_impaired) facts.push('听障')
  if (track.is_visual_impaired) facts.push('视障')
  return facts
}

function formatBitRate(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)} Mbps`
  return `${Math.round(value / 1000)} kbps`
}

function formatNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
}
