import type { Media } from '../types'
import type { HistoryItem } from '../types/history'

export const episodeIdentity = (media: Media): string => media.metadata_id || media.id

export function episodePresentation(media: Media): { title: string; subtitle: string } {
  if (media.metadata_kind !== 'episode') return { title: media.title, subtitle: '' }
  const title = media.series_title?.trim() || media.title
  const season = media.season_num === 0 ? '特别篇' : media.season_num > 0 ? `第 ${media.season_num} 季` : ''
  const episode = media.episode_num > 0 ? `第 ${media.episode_num} 集` : ''
  const ownTitle = media.title?.trim()
  const generic = /^(?:第\s*[0-9一二三四五六七八九十百零〇两]+\s*集|episode[\s._-]*\d+)$/i.test(ownTitle ?? '')
  return { title, subtitle: [season, episode, ownTitle && ownTitle !== title && !generic ? ownTitle : ''].filter(Boolean).join(' · ') }
}

export const episodeLabel = (media: Media): string => media.episode_num > 0
  ? `第 ${media.episode_num} 集`
  : media.metadata_kind === 'series' ? '整剧关联文件' : media.metadata_kind === 'season' ? '季关联文件' : '未识别集号'

export function distinctEpisodes(items: Media[]): Media[] {
  return [...new Map(items.map((item) => [episodeIdentity(item), item])).values()]
}

export function seriesResumeEpisode(items: Media[], history: HistoryItem[]): Media | undefined {
  const episodes = distinctEpisodes(items).sort((a, b) => a.season_num - b.season_num || a.episode_num - b.episode_num)
  const latest = history.find((row) => episodes.some((ep) => episodeIdentity(ep) === row.metadata_id))
  if (!latest) return episodes.find((ep) => ep.season_num > 0) ?? episodes[0]
  const index = episodes.findIndex((ep) => episodeIdentity(ep) === latest.metadata_id)
  if (latest.completed) return episodes.slice(index + 1).find((ep) => !history.some((row) => row.metadata_id === episodeIdentity(ep) && row.completed)) ?? episodes[index]
  return items.find((ep) => ep.id === latest.media_id) ?? episodes[index]
}

export function resolveSeriesSelection(seasons: { season: number; episodes: Media[] }[], params: URLSearchParams) {
  const raw = params.get('season')
  const requestedSeason = raw !== null && /^\d+$/.test(raw) ? Number(raw) : null
  const season = seasons.find((group) => group.season === requestedSeason) ?? seasons.find((group) => group.season > 0) ?? seasons[0]
  const requestedEpisode = params.get('episode')
  const episode = season?.episodes.find((ep) => episodeIdentity(ep) === requestedEpisode || ep.id === requestedEpisode) ?? season?.episodes[0]
  return { season, episode }
}
