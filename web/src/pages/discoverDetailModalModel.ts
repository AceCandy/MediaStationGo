import type { DiscoverItem } from '../api/discover'

export function discoverItemMetaText(item: DiscoverItem): string {
  return [
    item.media_type,
    item.year && item.year > 0 ? item.year : '',
    item.rating ? `★ ${item.rating.toFixed(1)}` : '',
  ]
    .filter(Boolean)
    .join(' · ')
}
