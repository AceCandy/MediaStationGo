import { MetadataEditDialog } from '../components/MetadataEditDialog'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'

type LibraryPageDialogsProps = {
  seriesMetadataEditOpen: boolean
  selectedSeries: SeriesCard | null
  selectedSeriesMediaIDs: string[]
  onCloseSeriesMetadataEdit: () => void
  onApplied: () => void
}

export function LibraryPageDialogs({
  seriesMetadataEditOpen,
  selectedSeries,
  selectedSeriesMediaIDs,
  onCloseSeriesMetadataEdit,
  onApplied,
}: LibraryPageDialogsProps) {
  const selectedSeriesTitle = selectedSeries ? seriesTitle(selectedSeries.rep) : ''

  return (
    <MetadataEditDialog
      open={seriesMetadataEditOpen}
      media={selectedSeries?.rep ?? null}
      mediaIds={selectedSeriesMediaIDs}
      mode="series"
      scopeLabel={selectedSeriesTitle || '当前剧集'}
      onClose={onCloseSeriesMetadataEdit}
      onSaved={onApplied}
    />
  )
}
