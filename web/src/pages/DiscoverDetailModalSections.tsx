import { X } from 'lucide-react'

import type { DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'
import { discoverItemMetaText } from './discoverDetailModalModel'

export function DiscoverModalHeader({ item, source, onClose }: { item: DiscoverItem; source: string; onClose: () => void }) {
  return (
    <div className="mb-4 flex items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-xs font-semibold uppercase tracking-widest text-brand-500">{source}</p>
        <h2 className="break-words font-display text-2xl font-bold text-ink-600">{item.title}</h2>
        <p className="mt-1 text-sm text-sand-500">{discoverItemMetaText(item)}</p>
      </div>
      <button type="button" aria-label="关闭详情" className="icon-btn shrink-0" onClick={onClose}>
        <X size={18} />
      </button>
    </div>
  )
}

export function DiscoverArtworkPanel({ item }: { item: DiscoverItem }) {
  return (
    <div className="space-y-3">
      <div className="overflow-hidden rounded-2xl bg-gray-100">
        {item.poster_url ? (
          <img src={imageURL(item.poster_url)} alt={item.title} className="aspect-[2/3] w-full object-cover" />
        ) : (
          <div className="flex aspect-[2/3] items-center justify-center text-sand-500">无海报</div>
        )}
      </div>
    </div>
  )
}
