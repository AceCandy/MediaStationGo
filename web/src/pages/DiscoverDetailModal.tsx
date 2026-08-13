import type { DiscoverItem } from '../api/discover'
import { ModalShell } from '../components/ModalShell'
import { discoverItemSource } from './discoverPageModel'
import {
  DiscoverArtworkPanel,
  DiscoverModalHeader,
  DiscoverOverviewPanel,
} from './DiscoverDetailModalSections'

export function DiscoverDetailModal({ item, onClose }: { item: DiscoverItem; onClose: () => void }) {
  const source = discoverItemSource(item)

  return (
    <ModalShell maxWidth="max-w-5xl" className="max-h-[92vh] overflow-y-auto p-5" ariaLabel={item.title}>
      <DiscoverModalHeader item={item} source={source} onClose={onClose} />
      <div className="grid gap-5 lg:grid-cols-[260px_1fr]">
        <DiscoverArtworkPanel item={item} />
        <div className="space-y-5">
          <DiscoverOverviewPanel overview={item.overview} />
        </div>
      </div>
    </ModalShell>
  )
}
