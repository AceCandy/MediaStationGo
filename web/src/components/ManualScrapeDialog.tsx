import type { Media } from '../types'
import { isEpisodeArtworkTarget } from './ManualScrapeDialogModel'
import { ModalShell } from './ModalShell'
import {
  ManualScrapeCandidateList,
  ManualScrapeDialogHeader,
  ManualScrapeSearchControls,
} from './ManualScrapeDialogSections'
import { useManualScrapeDialogState } from './useManualScrapeDialogState'

interface ManualScrapeDialogProps {
  open: boolean
  media: Media | null
  mediaIds?: string[]
  defaultQuery?: string
  mediaType?: string
  scopeLabel?: string
  episodeArtwork?: boolean
  onClose: () => void
  onApplied?: () => void
}

export function ManualScrapeDialog({
  open,
  media,
  mediaIds,
  defaultQuery,
  mediaType,
  scopeLabel,
  episodeArtwork,
  onClose,
  onApplied,
}: ManualScrapeDialogProps) {
  const dialog = useManualScrapeDialogState({
    open,
    media,
    mediaIds,
    defaultQuery,
    mediaType,
    episodeArtwork,
    onClose,
    onApplied,
  })

  if (!open || !media) return null

  return (
    <ModalShell maxWidth="max-w-4xl" className="flex max-h-[86vh] flex-col" ariaLabel={scopeLabel || media.title}>
      <ManualScrapeDialogHeader title={scopeLabel || media.title} targetCount={dialog.targetIds.length} onClose={onClose} />

      <ManualScrapeSearchControls
        query={dialog.query}
        selectedProviders={dialog.selectedProviders}
        searching={dialog.searching}
        includeEpisodeArtwork={dialog.includeEpisodeArtwork}
        showEpisodeArtworkToggle={isEpisodeArtworkTarget(media, mediaType, dialog.targetIds.length)}
        onQueryChange={dialog.setQuery}
        onProviderChange={dialog.setSelectedProviders}
        onSearch={dialog.runSearch}
        onEpisodeArtworkChange={dialog.setIncludeEpisodeArtwork}
      />

      <div className="flex-1 overflow-y-auto p-5">
        <ManualScrapeCandidateList items={dialog.items} applyingKey={dialog.applyingKey} onApply={dialog.apply} />
      </div>
    </ModalShell>
  )
}
