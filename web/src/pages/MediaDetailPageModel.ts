import type { Media } from '../types'
import { getSeriesKey, isEpisodeLike } from '../utils/groupSeries'

export function mediaLibraryBackTarget(media: Media): string {
  const libraryID = media.display_library_id || media.library_id
  if (!libraryID) return ''
  if (!isEpisodeLike(media)) return `/library/${encodeURIComponent(libraryID)}`

  const seriesKey = getSeriesKey(media)
  const target = `/library/${encodeURIComponent(libraryID)}`
  return seriesKey ? `${target}?series=${encodeURIComponent(seriesKey)}` : target
}
