import { ArrowLeft, Play } from 'lucide-react'
import { Link } from 'react-router-dom'

import { ExternalPlayerButton } from '../components/ExternalPlayerButton'
import { MetadataEditDialog } from '../components/MetadataEditDialog'
import type { Media } from '../types'
import { MediaDetailAdminMenu } from './MediaDetailAdminPanel'
import { MediaDetailPoster } from './MediaDetailArtwork'
import { MediaDetailCast } from './MediaDetailCast'
import { MediaDetailMetadata } from './MediaDetailMetadata'
import { MediaDetailTracks } from './MediaDetailTracks'

interface MediaDetailPlaybackActionsProps {
  media: Media
}

interface MediaDetailMainContentProps extends MediaDetailPlaybackActionsProps {
  versions: Media[]
  displayMedia: Media | null
  selectedVersionID: string
  isAdmin: boolean
  mediaInfoLoading: boolean
  probing: boolean
  probeError: string
  favourite: boolean
  onVersionChange: (id: string) => void
  onToggleFavourite: () => void
  onTMDbRefresh: () => void
  tmdbRefreshPending: boolean
  onDoubanEnrich: () => void
  onDoubanBound: () => void | Promise<void>
  doubanEnrichmentPending: boolean
  onMetadataEdit: () => void
  onProbe: () => void
  onSoftDelete: () => void
}

interface MediaDetailDialogsProps {
  media: Media
  metadataEditOpen: boolean
  onMetadataEditClose: () => void
  onMetadataSaved: (media: Media) => void | Promise<void>
}

export function MediaDetailLoading() {
  return (
    <div className="flex items-center justify-center py-48">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-gray-100 border-t-gray-900" />
    </div>
  )
}

export function MediaDetailMissing() {
  return (
    <div className="text-center py-24 bg-white rounded-2xl border border-gray-200">
      <p className="text-gray-500">媒体资源已被移除或不存在</p>
    </div>
  )
}

export function MediaDetailBackButton({ onBack }: { onBack: () => void }) {
  return (
    <div className="relative z-20 px-6 pt-6 sm:px-10 sm:pt-8">
      <button
        type="button"
        onClick={onBack}
        className="btn-ghost gap-2 bg-white/80 shadow-sm backdrop-blur hover:bg-white"
      >
        <ArrowLeft size={16} />
        <span>返回媒体库</span>
      </button>
    </div>
  )
}

export function MediaDetailPlaybackActions({
  media,
}: MediaDetailPlaybackActionsProps) {
  return (
    <div className="flex flex-wrap gap-3">
      <Link to={`/play/${media.id}`} className="btn-primary px-6 py-3.5 shadow-sm">
        <Play size={16} fill="currentColor" />
        <span>立即播放</span>
      </Link>

      <ExternalPlayerButton mediaId={media.id} />
    </div>
  )
}

export function MediaDetailMainContent({
  media,
  versions,
  displayMedia,
  selectedVersionID,
  isAdmin,
  mediaInfoLoading,
  probing,
  probeError,
  favourite,
  onVersionChange,
  onToggleFavourite,
  onTMDbRefresh,
  tmdbRefreshPending,
  onDoubanEnrich,
  onDoubanBound,
  doubanEnrichmentPending,
  onMetadataEdit,
  onProbe,
  onSoftDelete,
}: MediaDetailMainContentProps) {
  return (
    <>
      <div className="relative z-20 flex flex-col gap-8 p-6 pt-10 sm:p-10 sm:pt-16 md:flex-row lg:gap-12">
        {/* 左列：海报 + 媒体信息，紧跟海报不留空 */}
        <div className="mx-auto flex w-56 shrink-0 flex-col gap-7 md:mx-0 lg:w-64">
          <MediaDetailPoster media={media} />
          <MediaDetailTracks
            media={displayMedia}
            versions={versions}
            selectedVersionID={selectedVersionID}
            loading={mediaInfoLoading}
            probing={probing}
            probeError={probeError}
            onVersionChange={onVersionChange}
          />
        </div>

        {/* 右列：元信息 → 播放操作（管理操作收敛进「更多操作」） */}
        <div className="min-w-0 flex-1 space-y-7">
          <MediaDetailMetadata
            media={media}
            selectedMedia={displayMedia ?? versions.find((version) => version.id === selectedVersionID) ?? media}
            isAdmin={isAdmin}
            favourite={favourite}
            onToggleFavourite={onToggleFavourite}
            onMetadataEdit={onMetadataEdit}
            onDoubanBound={onDoubanBound}
          />
          <div className="divider border-gray-200/60" />
          <div className="flex flex-wrap items-center gap-3">
            <MediaDetailPlaybackActions
              media={displayMedia ?? media}
            />
            {isAdmin && (
              <MediaDetailAdminMenu
                onTMDbRefresh={media.catalog_source ? undefined : onTMDbRefresh}
                tmdbRefreshPending={tmdbRefreshPending}
                onDoubanEnrich={(media.metadata_kind === 'movie' || media.metadata_kind === 'series') && media.douban_id ? onDoubanEnrich : undefined}
                doubanEnrichmentPending={doubanEnrichmentPending}
                doubanDegraded={media.douban_status === 'degraded'}
                onMetadataEdit={onMetadataEdit}
                onProbe={onProbe}
                onSoftDelete={onSoftDelete}
              />
            )}
          </div>
        </div>
      </div>

      {/* 演职员：通栏置于最底部（层级低于上栏，避免遮挡轨道下拉菜单） */}
      <div className="relative z-10 px-6 pb-8 sm:px-10 sm:pb-10">
        <MediaDetailCast mediaId={media.id} />
      </div>
    </>
  )
}

export function MediaDetailDialogs({
  media,
  metadataEditOpen,
  onMetadataEditClose,
  onMetadataSaved,
}: MediaDetailDialogsProps) {
  return (
    <>
      <MetadataEditDialog
        open={metadataEditOpen}
        media={media}
        onClose={onMetadataEditClose}
        onSaved={onMetadataSaved}
      />
    </>
  )
}
