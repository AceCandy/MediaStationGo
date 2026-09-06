import { useEffect, useRef, useState } from 'react'
import { Captions, Check, ChevronDown, Layers3, LoaderCircle, Music2, Video, type LucideIcon } from 'lucide-react'

import type { Media, MediaTrack } from '../types'
import { formatSize } from './libraryPageModel'

type MediaDetailTracksProps = {
  media: Media | null
  versions: Media[]
  selectedVersionID: string
  loading: boolean
  probing: boolean
  probeError: string
  onVersionChange: (id: string) => void
  readOnlyTracks?: boolean
}

export function MediaDetailTracks({
  media,
  versions,
  selectedVersionID,
  loading,
  probing,
  probeError,
  onVersionChange,
  readOnlyTracks = false,
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

      <div className="grid grid-cols-1 gap-3">
        <MediaInfoPicker
          icon={Layers3}
          label="版本"
          shellClass="border-[var(--app-border)] bg-[var(--app-control-bg)]"
          textClass="text-brand-500"
          options={versions.map((version, index) => ({
            value: version.id,
            label: mediaVersionLabel(version, index, versions.length),
          }))}
          value={selectedVersionID}
          placeholder="暂无可用版本"
          onChange={onVersionChange}
        />

        {readOnlyTracks ? (['video', 'audio', 'subtitle'] as const).map((type) => type === 'subtitle' ? <TrackSelect key={type} type={type} label="字幕" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} /> : <MediaInfoPicker
          key={type}
          icon={trackStyles[type].icon}
          label={type === 'video' ? '视频' : '音频'}
          shellClass={trackStyles[type].shell}
          textClass={trackStyles[type].text}
          options={tracks.filter((track) => track.type === type).map((track) => ({ value: String(track.index), label: trackLabel(track) }))}
          value=""
          placeholder={unavailableText || '暂无轨道'}
          readOnly
        />) : <>
        <TrackSelect type="video" label="视频" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
        <TrackSelect type="audio" label="音频" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
        <TrackSelect type="subtitle" label="字幕" mediaID={media?.id} tracks={tracks} unavailableText={unavailableText} />
        </>}
      </div>
      {readOnlyTracks && <p className="text-xs text-[var(--app-muted)]">实际音轨与字幕请在播放器中选择。</p>}

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
  const style = trackStyles[type]
  const initialValue = selected ? String(selected.index) : ''
  const [selectedValue, setSelectedValue] = useState(initialValue)

  useEffect(() => setSelectedValue(initialValue), [initialValue, mediaID])

  return (
    <MediaInfoPicker
      icon={style.icon}
      label={label}
      shellClass={style.shell}
      textClass={style.text}
      options={items.map((track) => ({ value: String(track.index), label: trackLabel(track) }))}
      value={selectedValue}
      placeholder={placeholder}
      onChange={setSelectedValue}
    />
  )
}

function MediaInfoPicker({
  icon: Icon,
  label,
  shellClass,
  textClass,
  options,
  value,
  placeholder,
  onChange,
  readOnly = false,
}: {
  icon: LucideIcon
  label: string
  shellClass: string
  textClass: string
  options: Array<{ value: string; label: string }>
  value: string
  placeholder: string
  onChange?: (value: string) => void
  readOnly?: boolean
}) {
  const selected = options.find((option) => option.value === value) ?? options[0]
  const selectable = options.length > 1

  return (
    <div
      className={`relative flex min-h-12 min-w-0 items-center gap-2 rounded-xl border px-2.5 transition focus-within:border-brand-500 focus-within:ring-4 focus-within:ring-brand-500/10 ${shellClass}`}
    >
      <span className={`grid size-8 shrink-0 place-items-center rounded-lg bg-[var(--app-glass)] ${textClass}`} aria-hidden="true">
        <Icon size={18} />
      </span>
      <span className={`shrink-0 text-xs font-bold ${textClass}`}>{label}</span>

      {readOnly ? <div className="min-w-0 flex-1 space-y-1 break-words py-3 text-sm font-semibold text-[var(--app-text)]">
        {options.length > 0 ? options.map((option) => <p key={option.value}>{option.label}</p>) : <p className="text-[var(--app-muted)]">{placeholder}</p>}
      </div> : selectable ? (
        <details
          className="group min-w-0 flex-1"
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false
          }}
          onKeyDown={(event) => {
            if (event.key !== 'Escape') return
            event.preventDefault()
            event.currentTarget.open = false
            event.currentTarget.querySelector('summary')?.focus()
          }}
        >
          <summary
            className="flex min-w-0 cursor-pointer list-none items-center gap-2 py-3 text-left text-sm font-semibold text-[var(--app-text)] outline-none [&::-webkit-details-marker]:hidden"
            aria-label={`${label}：${selected?.label ?? placeholder}`}
          >
            <PickerValue text={selected?.label ?? placeholder} />
            <ChevronDown size={16} className="shrink-0 text-[var(--app-muted)] transition-transform group-open:rotate-180" aria-hidden="true" />
          </summary>
          <div
            role="group"
            aria-label={`${label}选项`}
            className="absolute inset-x-0 top-full z-50 mt-2 max-h-64 overflow-y-auto rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-1.5 shadow-xl"
          >
            {options.map((option) => {
              const active = option.value === selected?.value
              return (
                <button
                  key={option.value}
                  type="button"
                  aria-pressed={active}
                  className={`flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-sm font-semibold transition-colors ${active ? 'bg-brand-500/15 text-brand-500' : 'text-[var(--app-text)] hover:bg-[var(--app-hover)]'}`}
                  onClick={(event) => {
                    onChange?.(option.value)
                    const details = event.currentTarget.closest('details')
                    if (details) details.open = false
                    details?.querySelector('summary')?.focus()
                  }}
                >
                  <DropdownOptionValue text={option.label} />
                  {active && <Check size={16} className="shrink-0" aria-hidden="true" />}
                </button>
              )
            })}
          </div>
        </details>
      ) : (
        <PickerValue
          text={selected?.label ?? placeholder}
          muted={!selected}
        />
      )}
    </div>
  )
}

// PickerValue 截断文本 + hover 全文气泡：只有真的被截断时才显示 tooltip。
function PickerValue({ text, muted = false }: { text: string; muted?: boolean }) {
  const textRef = useRef<HTMLSpanElement>(null)
  const [truncated, setTruncated] = useState(false)

  useEffect(() => {
    const el = textRef.current
    if (el) setTruncated(el.scrollWidth > el.clientWidth + 1)
  }, [text])

  return (
    <span className="group/value relative min-w-0 flex-1 py-3">
      <span
        ref={textRef}
        className={`block truncate text-sm font-semibold ${muted ? 'text-[var(--app-muted)]' : 'text-[var(--app-text)]'}`}
      >
        {text}
      </span>
      {truncated && (
        <span
          role="tooltip"
          className="pointer-events-none absolute bottom-full left-0 z-[60] mb-2 max-w-80 whitespace-normal break-words rounded-lg bg-[var(--app-tooltip-bg)] px-2.5 py-1.5 text-xs font-semibold text-[var(--app-tooltip-text)] opacity-0 shadow-lg transition-opacity duration-200 group-hover/value:opacity-100"
        >
          {text}
        </span>
      )}
    </span>
  )
}

// DropdownOptionValue 下拉选项文本：截断时 hover 用 fixed 定位气泡显示全文，
// 不被下拉列表的 overflow 滚动容器裁剪；滚动时立即收起避免错位。
// 注意：<details> 收起期间选项不排版（clientWidth 为 0），截断必须在 hover 时现测。
function DropdownOptionValue({ text }: { text: string }) {
  const textRef = useRef<HTMLSpanElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number; maxWidth: number } | null>(null)

  useEffect(() => {
    if (!pos) return undefined
    const hide = () => setPos(null)
    window.addEventListener('scroll', hide, true)
    return () => window.removeEventListener('scroll', hide, true)
  }, [pos])

  return (
    <span
      className="relative min-w-0 flex-1"
      onPointerEnter={() => {
        const el = textRef.current
        if (!el || el.scrollWidth <= el.clientWidth + 1) return
        const rect = el.getBoundingClientRect()
        const left = rect.right + 10
        const maxWidth = Math.max(140, Math.min(320, window.innerWidth - left - 12))
        setPos({ top: rect.top + rect.height / 2, left, maxWidth })
      }}
      onPointerLeave={() => setPos(null)}
    >
      <span ref={textRef} className="block truncate">{text}</span>
      {pos && (
        <span
          role="tooltip"
          className="pointer-events-none fixed z-[70] -translate-y-1/2 whitespace-normal break-words rounded-lg bg-[var(--app-tooltip-bg)] px-2.5 py-1.5 text-xs font-semibold text-[var(--app-tooltip-text)] shadow-lg"
          style={{ top: pos.top, left: pos.left, maxWidth: pos.maxWidth }}
        >
          {text}
        </span>
      )}
    </span>
  )
}

const trackStyles = {
  video: {
    icon: Video,
    shell: 'border-sage-500/25 bg-sage-500/10',
    text: 'text-sage-500',
  },
  audio: {
    icon: Music2,
    shell: 'border-brand-500/25 bg-brand-500/10',
    text: 'text-brand-500',
  },
  subtitle: {
    icon: Captions,
    shell: 'border-gold-500/25 bg-gold-500/10',
    text: 'text-gold-600',
  },
} as const

function mediaVersionLabel(media: Media, index: number, count: number): string {
  const facts = count > 1 ? [`#${index + 1}`] : []
  const resolution = resolutionLabel(media.width, media.height)
  if (resolution) facts.push(resolution)
  if (media.video_codec) facts.push(media.video_codec.toUpperCase())
  if (media.container) facts.push(media.container.toUpperCase())
  if (media.size_bytes > 0) facts.push(formatSize(media.size_bytes))
  return facts.length > 0 ? facts.join(' · ') : `版本 ${index + 1}`
}

function trackLabel(track: MediaTrack): string {
  const facts: string[] = []
  if (track.type === 'video') {
    const resolution = resolutionLabel(track.width, track.height)
    if (resolution) facts.push(resolution)
    if (track.video_range && track.video_range !== 'SDR') facts.push(track.video_range)
    if (track.codec) facts.push(track.codec.toUpperCase())
  } else if (track.type === 'audio') {
    if (track.display_language || track.language) facts.push(track.display_language || track.language || '')
    if (track.codec) facts.push(track.codec.toUpperCase())
    if (track.channel_layout) facts.push(track.channel_layout)
    else if (track.channels) facts.push(`${track.channels} 声道`)
    if (track.is_visual_impaired) facts.push('视障解说')
  } else {
    if (track.display_language || track.language || track.title) {
      facts.push(track.display_language || track.language || track.title || '')
    }
    if (track.codec) facts.push(track.codec.toUpperCase())
    if (track.is_hearing_impaired) facts.push('听障')
    if (track.is_forced) facts.push('强制')
  }
  if (track.is_default) facts.push('默认')
  return facts.length > 0 ? facts.join(' · ') : track.display_title || track.title || track.codec || '未知轨道'
}

function resolutionLabel(width = 0, height = 0): string {
  if (width >= 7000 || height >= 4000) return '8K'
  if (width >= 3800 || height >= 2000) return '4K'
  if (height >= 1080) return '1080p'
  if (height >= 720) return '720p'
  return width > 0 && height > 0 ? `${width} × ${height}` : ''
}
