import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ChevronRight,
  Film,
  FolderOpen,
  Image,
  LibraryBig,
  Plus,
  Save,
  Trash2,
  X,
} from 'lucide-react'

import { ModalShell } from '../components/ModalShell'
import { imageURL } from '../api/client'
import type { Library, LibraryRoot } from '../types'
import type { RootDraft } from './adminLibraryPanelModel'
import { displayLibraryRootPath, emptyRootDraft, fallbackLibraryRoot } from './adminLibraryPanelModel'
import { LibraryRootPathField } from './AdminLibraryPanelSections'

const LIBRARY_TYPE_LABELS: Record<string, string> = {
  movie: '电影',
  tv: '电视剧',
  variety: '综艺',
  anime: '动漫',
  music: '音乐',
  nfo_movie: '非常规电影',
  nfo_tv: '非常规剧集',
}

function libraryTypeLabel(type: string): string {
  return LIBRARY_TYPE_LABELS[type] ?? type
}

type LibraryActionProps = {
  onToggleRoot: (libraryID: string, root: LibraryRoot) => void
  onRemoveRoot: (library: Library, root: LibraryRoot) => void
  onRemoveLibrary: (library: Library) => void
  onAddLibraryRoot: (library: Library, root: RootDraft) => Promise<boolean>
  onUploadLibraryCover: (library: Library, cover: File) => Promise<void>
  onClearLibraryCover: (library: Library) => Promise<void>
}

/* ── 媒体库卡片网格 ── */

export function AdminLibraryGrid({
  libs,
  onSelect,
}: {
  libs: Library[]
  onSelect?: (library: Library) => void
}) {
  if (!libs.length) {
    return (
      <div className="glass-panel flex flex-col items-center gap-2 py-12 text-center">
        <div className="modal-icon">
          <LibraryBig size={20} />
        </div>
        <p className="font-medium text-ink-600">还没有媒体库</p>
        <p className="text-sm text-ink-50">
          {onSelect ? '点击右上角「新建媒体库」创建第一个媒体库。' : '暂无可浏览的媒体库。'}
        </p>
      </div>
    )
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4">
      {libs.map((library) => (
        <LibraryGridCard key={library.id} library={library} onSelect={onSelect} />
      ))}
    </div>
  )
}

function LibraryGridCard({
  library,
  onSelect,
}: {
  library: Library
  onSelect?: (library: Library) => void
}) {
  const rootCount = library.roots?.length || 1
  const details = (
    <>
      <div className="min-w-0">
        <h3 className="truncate font-display text-base font-bold text-white">{library.name}</h3>
        <p className="mt-0.5 flex items-center gap-1 text-xs text-gray-300">
          <FolderOpen size={12} /> {rootCount} 个路径来源
        </p>
      </div>
      {onSelect && <ChevronRight size={16} className="shrink-0 text-gray-300" />}
    </>
  )
  return (
    <article className="card-hover group relative aspect-video overflow-hidden !p-0 text-left">
      <Link
        to={`/library/${library.id}`}
        aria-label={`浏览媒体库 ${library.name}`}
        className="absolute inset-0 flex items-center justify-center bg-brand-50"
      >
        {library.cover_url ? (
          <img
            src={imageURL(library.cover_url)}
            alt=""
            className="h-full w-full object-cover object-center transition duration-300 group-hover:scale-105"
          />
        ) : (
          <Film size={32} className="text-brand-300" />
        )}
        <span className="badge-sage absolute left-3 top-3 opacity-100 shadow-sm transition-opacity [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover:opacity-100 [@media(hover:hover)]:group-focus-within:opacity-100">
          {libraryTypeLabel(library.type)}
        </span>
        {!library.enabled && (
          <span className="badge-neutral absolute right-3 top-3 shadow-sm">已禁用</span>
        )}
      </Link>
      {onSelect ? (
        <button
          type="button"
          onClick={() => onSelect(library)}
          aria-label={`设置媒体库 ${library.name}`}
          className="absolute inset-x-0 bottom-0 z-10 flex h-1/3 items-center justify-between gap-2 bg-black/70 px-4 text-left opacity-100 backdrop-blur-sm transition-opacity [@media(hover:hover)]:pointer-events-none [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover:pointer-events-auto [@media(hover:hover)]:group-hover:opacity-100 [@media(hover:hover)]:group-focus-within:pointer-events-auto [@media(hover:hover)]:group-focus-within:opacity-100"
        >
          {details}
        </button>
      ) : (
        <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 flex h-1/3 items-center justify-between gap-2 bg-black/70 px-4 opacity-100 backdrop-blur-sm transition-opacity [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover:opacity-100 [@media(hover:hover)]:group-focus-within:opacity-100">
          {details}
        </div>
      )}
    </article>
  )
}

/* ── 媒体库管理弹窗 ── */

type LibraryDetailDialogProps = LibraryActionProps & {
  library: Library
  onClose: () => void
}

export function LibraryDetailDialog({ library, onClose, ...actions }: LibraryDetailDialogProps) {
  const roots = library.roots?.length ? library.roots : [fallbackLibraryRoot(library)]
  const [newRoot, setNewRoot] = useState<RootDraft | null>(null)

  const saveNewRoot = async () => {
    if (newRoot && await actions.onAddLibraryRoot(library, newRoot)) setNewRoot(null)
  }

  return (
    <ModalShell
      onClose={onClose}
      maxWidth="max-w-2xl"
      ariaLabel={`管理媒体库 ${library.name}`}
      className="flex max-h-[85vh] flex-col"
    >
      <div className="modal-header">
        <div className="flex min-w-0 items-center gap-3">
          <div className="group/cover relative h-14 w-11 shrink-0">
            <label
              className="block h-full w-full cursor-pointer overflow-hidden rounded-lg border border-gray-200/80 focus-within:ring-2 focus-within:ring-brand-200"
              title="上传封面"
            >
              {library.cover_url ? (
                <img src={imageURL(library.cover_url)} alt="" className="h-full w-full object-cover" />
              ) : (
                <span className="flex h-full w-full items-center justify-center text-ink-50">
                  <Image size={20} />
                </span>
              )}
              <input
                type="file"
                className="absolute inset-0 cursor-pointer opacity-0"
                aria-label="上传媒体库封面"
                accept="image/jpeg,image/png,image/webp,image/gif,image/bmp"
                onChange={(event) => {
                  const cover = event.target.files?.[0]
                  event.target.value = ''
                  if (cover) void actions.onUploadLibraryCover(library, cover)
                }}
              />
            </label>
            {library.cover_url && (
              <button
                type="button"
                className="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full bg-black/70 text-white opacity-100 shadow transition-opacity [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/cover:opacity-100 focus:opacity-100"
                title="清除封面"
                aria-label="清除媒体库封面"
                onClick={() => void actions.onClearLibraryCover(library)}
              >
                <X size={12} />
              </button>
            )}
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="truncate font-display text-lg font-bold text-ink-600">{library.name}</h3>
              <span className="badge-sage">{libraryTypeLabel(library.type)}</span>
              {!library.enabled && <span className="badge-neutral">已禁用</span>}
            </div>
            <p className="mt-0.5 text-xs text-ink-50">{roots.length} 个路径来源</p>
          </div>
        </div>
        <button className="icon-btn" onClick={onClose} aria-label="关闭">
          <X size={18} />
        </button>
      </div>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-5 py-4">
        <p className="text-2xs font-bold uppercase tracking-widest text-ink-50">路径来源</p>
        {roots.map((root) => (
          <ExistingRootEditor key={root.id || root.path} library={library} root={root} {...actions} />
        ))}
        {newRoot ? (
          <div className="flex flex-col gap-2 rounded-xl border border-brand-300/60 bg-brand-50 p-2.5 lg:flex-row lg:items-center">
            <div className="flex min-w-0 flex-1 items-start gap-2">
              <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white/80 text-ink-50">
                <FolderOpen size={14} />
              </div>
              <LibraryRootPathField
                root={newRoot}
                pathRequired
                onChange={(patch) => setNewRoot((root) => (root ? { ...root, ...patch } : root))}
              />
            </div>
            <div className="flex justify-end gap-2">
              <button type="button" className="btn-outline px-3 py-2 shadow-none" onClick={() => setNewRoot(null)}>
                取消
              </button>
              <button type="button" className="btn-primary px-3 py-2" onClick={() => void saveNewRoot()}>
                <Save size={14} /> 保存路径
              </button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-gray-300 px-3 py-2.5 text-sm font-semibold text-ink-50 transition hover:border-brand-400 hover:text-brand-500"
            onClick={() => setNewRoot(emptyRootDraft())}
          >
            <Plus size={15} /> 添加路径
          </button>
        )}
      </div>

      <div className="modal-footer !justify-start">
        <button
          className="btn-ghost !text-red-500 hover:!bg-red-50 hover:!text-red-600"
          onClick={() => actions.onRemoveLibrary(library)}
        >
          <Trash2 size={14} /> 删除媒体库
        </button>
      </div>
    </ModalShell>
  )
}

/* ── 路径来源编辑 ── */

type RootEditorProps = Omit<LibraryDetailDialogProps, 'library' | 'onClose'> & {
  library: Library
  root: LibraryRoot
}

function ExistingRootEditor({ library, root, ...actions }: RootEditorProps) {
  return (
    <div className="flex flex-col gap-2 rounded-xl border border-gray-200/80 bg-gray-50/60 p-2.5 lg:flex-row lg:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-2">
        <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white/80 text-ink-50">
          <FolderOpen size={14} />
        </div>
        <div className="min-w-0 flex-1">
          <ReadonlyRootFields root={root} />
        </div>
      </div>
      <div className="flex items-center justify-end gap-2">
        <RootActionButtons library={library} root={root} {...actions} />
      </div>
    </div>
  )
}

function ReadonlyRootFields({ root }: { root: LibraryRoot }) {
  return (
    <span
      className="block min-w-0 truncate rounded-md bg-white/80 px-2.5 py-1.5 font-mono text-sm text-ink-100"
      title={displayLibraryRootPath(root.path)}
    >
      {displayLibraryRootPath(root.path)}
    </span>
  )
}

function RootStatus({ enabled, onClick }: { enabled: boolean; onClick?: () => void }) {
  const action = enabled ? '禁用' : '启用'
  const className = `inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-2xs font-bold ${
    enabled
      ? 'border-emerald-300/60 bg-emerald-50 text-emerald-600'
      : 'border-gray-300 bg-gray-100 text-ink-50'
  }`
  const content = (
    <>
      <span className={`h-1.5 w-1.5 rounded-full ${enabled ? 'bg-emerald-500' : 'bg-gray-400'}`} />
      {enabled ? '启用' : '禁用'}
    </>
  )
  if (!onClick) return <span className={className}>{content}</span>
  return (
    <button
      type="button"
      className={`${className} transition hover:border-brand-400`}
      title={`${action}路径`}
      aria-label={`${action}路径`}
      onClick={onClick}
    >
      {content}
    </button>
  )
}

function RootActionButtons({ library, root, ...actions }: RootEditorProps) {
  if (!root.id) return <RootStatus enabled={root.enabled} />
  return (
    <>
      <RootStatus enabled={root.enabled} onClick={() => actions.onToggleRoot(library.id, root)} />
      <button
        type="button"
        className="flex h-8 w-8 items-center justify-center rounded-lg border border-red-300/60 text-red-500 transition hover:bg-red-50"
        title="删除路径"
        aria-label="删除路径"
        onClick={() => actions.onRemoveRoot(library, root)}
      >
        <Trash2 size={14} />
      </button>
    </>
  )
}
