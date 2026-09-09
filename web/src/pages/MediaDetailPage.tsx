import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { LoaderCircle } from 'lucide-react'

import { ModalShell } from '../components/ModalShell'
import { useAuthStore } from '../stores/auth'
import { MediaDetailBackdrop } from './MediaDetailArtwork'
import {
  MediaDetailBackButton,
  MediaDetailDialogs,
  MediaDetailLoading,
  MediaDetailMainContent,
  MediaDetailMissing,
} from './MediaDetailPageSections'
import { useMediaDetailPageState } from './useMediaDetailPageState'

export function MediaDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const user = useAuthStore((s) => s.user)
  const state = location.state as { from?: unknown } | null
  const backTarget = typeof state?.from === 'string' && state.from.startsWith('/library/') ? state.from : ''
  const detail = useMediaDetailPageState({ id, navigate, backTarget })

  if (detail.loading && !detail.tmdbRefreshPending) return <MediaDetailLoading />
  if (!detail.media) return <MediaDetailMissing />
  const media = detail.media

  return (
    <>
      {detail.tmdbRefreshPending && (
        <ModalShell ariaLabel="正在刷新 TMDB 信息" maxWidth="max-w-sm" zIndex={100}>
          <div
            ref={(element) => element?.focus()}
            tabIndex={-1}
            onKeyDown={(event) => { if (event.key === 'Tab') event.preventDefault() }}
            className="flex flex-col items-center gap-4 p-8 text-center outline-none"
          >
            <LoaderCircle size={32} aria-hidden="true" className="animate-spin text-brand-500 motion-reduce:animate-none" />
            <p role="status" className="text-sm text-[var(--app-text)]">正在刷新 TMDB 信息，请稍候…</p>
          </div>
        </ModalShell>
      )}
      {detail.loading ? <MediaDetailLoading /> : (
        <div className="relative overflow-hidden rounded-3xl bg-white border border-gray-200/90 shadow-[0_1px_3px_rgba(0,0,0,0.01),0_1px_2px_rgba(0,0,0,0.015)]">
          <MediaDetailBackdrop media={media} />

          <MediaDetailBackButton onBack={detail.goBack} />

          <MediaDetailMainContent
            media={media}
            versions={detail.versions}
            displayMedia={detail.displayMedia}
            selectedVersionID={detail.selectedVersionID}
            isAdmin={user?.role === 'admin'}
            mediaInfoLoading={detail.mediaInfoLoading}
            probing={detail.probing}
            probeError={detail.probeError}
            favourite={detail.favourite}
            onVersionChange={detail.selectVersion}
            onToggleFavourite={detail.toggleFavourite}
            onTMDbRefresh={detail.refreshTMDb}
            tmdbRefreshPending={detail.tmdbRefreshPending}
            onDoubanEnrich={detail.enrichDouban}
            onDoubanBound={() => detail.handleMetadataSaved(media)}
            doubanEnrichmentPending={detail.doubanEnrichmentPending}
            onMetadataEdit={() => detail.setMetadataEditOpen(true)}
            onProbe={detail.reprobe}
            onSoftDelete={detail.softDelete}
          />
          <MediaDetailDialogs
            media={media}
            metadataEditOpen={detail.metadataEditOpen}
            onMetadataEditClose={() => detail.setMetadataEditOpen(false)}
            onMetadataSaved={detail.handleMetadataSaved}
          />
        </div>
      )}
    </>
  )
}
