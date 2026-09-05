import { MetadataEditDialog } from '../components/MetadataEditDialog'
import { useEffect, useState } from 'react'
import { mediaAPI } from '../api/library'
import { ModalShell } from '../components/ModalShell'
import type { Media } from '../types'
import { seriesTitle, type SeriesCard } from '../utils/groupSeries'

type LibraryPageDialogsProps = {
  seriesMetadataEditOpen: boolean
  selectedSeries: SeriesCard | null
  onCloseSeriesMetadataEdit: () => void
  onApplied: () => void
}

export function LibraryPageDialogs({
  seriesMetadataEditOpen,
  selectedSeries,
  onCloseSeriesMetadataEdit,
  onApplied,
}: LibraryPageDialogsProps) {
  const selectedSeriesTitle = selectedSeries ? seriesTitle(selectedSeries.rep) : ''
  const mediaID = selectedSeries?.rep.id
  const [media, setMedia] = useState<Media | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    setMedia(null)
    setFailed(false)
    if (!seriesMetadataEditOpen || !mediaID) return
    let cancelled = false
    mediaAPI.series(mediaID).then((data) => {
      if (!cancelled) setMedia({ ...data.series, id: mediaID })
    }).catch(() => { if (!cancelled) setFailed(true) })
    return () => { cancelled = true }
  }, [seriesMetadataEditOpen, mediaID])

  if (seriesMetadataEditOpen && (!media || media.id !== mediaID)) return (
    <ModalShell onClose={onCloseSeriesMetadataEdit} ariaLabel="加载整剧元数据" maxWidth="max-w-sm">
      <div className="space-y-4 p-6"><p role="status">{failed ? '整剧元数据加载失败，请关闭后重试。' : '正在加载整剧元数据…'}</p><button className="btn-outline" onClick={onCloseSeriesMetadataEdit}>关闭</button></div>
    </ModalShell>
  )

  return (
    <MetadataEditDialog
      open={seriesMetadataEditOpen}
      media={media}
      mode="series"
      scopeLabel={selectedSeriesTitle || '当前剧集'}
      onClose={onCloseSeriesMetadataEdit}
      onSaved={onApplied}
    />
  )
}
