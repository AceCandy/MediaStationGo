import type { Media } from '../types'

export function mediaLibraryBackTarget(media: Media): string {
  const libraryID = media.display_library_id || media.library_id
  if (!libraryID) return ''
  const target = `/library/${encodeURIComponent(libraryID)}`
  return media.series_id ? `${target}?series_id=${encodeURIComponent(media.series_id)}` : target
}
