import { useState, type MouseEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  ChevronRight,
  Film,
  FolderOpen,
  Image,
  LibraryBig,
  MoreVertical,
  Plus,
  Power,
  PowerOff,
  Save,
  Trash2,
  X,
} from 'lucide-react'

import { ModalShell } from '../components/ModalShell'
import { imageURL } from '../api/client'
import type { Library, LibraryRoot } from '../types'
import type { RootDraft } from './adminLibraryPanelModel'
import { displayLibraryRootName, displayLibraryRootPath, emptyRootDraft, fallbackLibraryRoot } from './adminLibraryPanelModel'
import { LibraryRootFields } from './AdminLibraryPanelSections'

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
  editableRootDraft: (libraryID: string, root: LibraryRoot) => RootDraft
  onEditableRootChange: (libraryID: string, root: LibraryRoot, patch: Partial<RootDraft>) => void
  onSaveRoot: (libraryID: string, root: LibraryRoot) => void
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
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
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
        <h3 className="truncate font-display text-base font-bold text-ink-600">{library.name}</h3>
        <p className="mt-0.5 flex items-center gap-1 text-xs text-ink-50">
          <FolderOpen size={12} /> {rootCount} 个路径来源
        </p>
      </div>
      {onSelect && (
        <ChevronRight
          size={16}
          className="shrink-0 text-ink-50 transition group-hover:translate-x-0.5 group-hover:text-brand-500"
        />
      )}
    </>
  )
  return (
    <article className="card-hover group overflow-hidden !p-0 text-left">
      <Link
        to={`/library/${library.id}`}
        aria-label={`浏览媒体库 ${library.name}`}
        className="relative flex h-32 items-center justify-center overflow-hidden bg-brand-50"
      >
        {library.cover_url ? (
          <img
            src={imageURL(library.cover_url)}
            alt=""
            className="h-full w-full object-cover transition duration-300 group-hover:scale-105"
          />
        ) : (
          <Film size={32} className="text-brand-300" />
        )}
        <span className="badge-sage absolute left-3 top-3 shadow-sm">
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
          className="flex w-full items-center justify-between gap-2 p-4 text-left"
        >
          {details}
        </button>
      ) : (
        <div className="flex items-center justify-between gap-2 p-4">{details}</div>
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
          {library.cover_url ? (
            <img
              src={imageURL(library.cover_url)}
              alt=""
              className="h-14 w-11 shrink-0 rounded-lg border border-gray-200/80 object-cover"
            />
          ) : (
            <div className="modal-icon">
              <Film size={20} />
            </div>
          )}
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
              <LibraryRootFields
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

      <div className="modal-footer flex-wrap !justify-between">
        <button
          className="btn-ghost !text-red-500 hover:!bg-red-50 hover:!text-red-600"
          onClick={() => actions.onRemoveLibrary(library)}
        >
          <Trash2 size={14} /> 删除媒体库
        </button>
        <div className="flex flex-col items-end gap-1.5">
          <span className="text-2xs text-ink-50">封面推荐 16:9，支持 JPG、PNG、WebP、GIF、BMP</span>
          <div className="flex flex-wrap justify-end gap-2">
            {library.cover_url && (
              <button
                type="button"
                className="btn-outline px-3 py-2 shadow-none"
                onClick={() => void actions.onClearLibraryCover(library)}
              >
                清除封面
              </button>
            )}
            <label className="btn-outline relative cursor-pointer overflow-hidden px-3 py-2 shadow-none focus-within:ring-2 focus-within:ring-brand-200">
              <Image size={14} /> 上传封面
              <input
                type="file"
                className="absolute inset-0 cursor-pointer opacity-0"
                accept="image/jpeg,image/png,image/webp,image/gif,image/bmp"
                onChange={(event) => {
                  const cover = event.target.files?.[0]
                  event.target.value = ''
                  if (cover) void actions.onUploadLibraryCover(library, cover)
                }}
              />
            </label>
          </div>
        </div>
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
  const draft = actions.editableRootDraft(library.id, root)
  return (
    <div className="flex flex-col gap-2 rounded-xl border border-gray-200/80 bg-gray-50/60 p-2.5 lg:flex-row lg:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-2">
        <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white/80 text-ink-50">
          <FolderOpen size={14} />
        </div>
        <div className="min-w-0 flex-1">
          {root.id ? (
            <EditableRootFields library={library} root={root} draft={draft} {...actions} />
          ) : (
            <ReadonlyRootFields root={root} />
          )}
        </div>
      </div>
      <div className="flex items-center justify-end gap-2">
        <RootStatus enabled={draft.enabled ?? root.enabled} />
        <RootActionButtons library={library} root={root} draft={draft} {...actions} />
      </div>
    </div>
  )
}

function ReadonlyRootFields({ root }: { root: LibraryRoot }) {
  return (
    <div className="min-w-0 space-y-1">
      <span className="block truncate rounded-md bg-white/80 px-2.5 py-1.5 text-xs text-ink-600">
        {displayLibraryRootName(root.name, root.path)}
      </span>
      <span
        className="block min-w-0 truncate rounded-md bg-white/80 px-2.5 py-1.5 font-mono text-2xs text-ink-100"
        title={displayLibraryRootPath(root.path)}
      >
        {displayLibraryRootPath(root.path)}
      </span>
    </div>
  )
}

function EditableRootFields({ library, root, draft, onEditableRootChange }: RootEditorProps & { draft: RootDraft }) {
  return (
    <LibraryRootFields root={draft} onChange={(patch) => onEditableRootChange(library.id, root, patch)} />
  )
}

function RootStatus({ enabled }: { enabled: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-2xs font-bold ${
        enabled
          ? 'border-emerald-300/60 bg-emerald-50 text-emerald-600'
          : 'border-gray-300 bg-gray-100 text-ink-50'
      }`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${enabled ? 'bg-emerald-500' : 'bg-gray-400'}`} />
      {enabled ? '启用' : '禁用'}
    </span>
  )
}

function RootActionButtons({ library, root, draft, ...actions }: RootEditorProps & { draft: RootDraft }) {
  const enabled = draft.enabled ?? root.enabled
  return (
    <ActionMenu label="路径操作">
      {root.id && (
        <MenuButton
          icon={<Save size={14} />}
          label="保存"
          onClick={() => actions.onSaveRoot(library.id, root)}
        >
          保存
        </MenuButton>
      )}
      {root.id && (
        <MenuButton
          icon={enabled ? <PowerOff size={14} /> : <Power size={14} />}
          label={enabled ? '禁用' : '启用'}
          onClick={() => actions.onToggleRoot(library.id, root)}
        >
          {enabled ? '禁用' : '启用'}
        </MenuButton>
      )}
      {root.id && (
        <MenuButton
          danger
          icon={<Trash2 size={14} />}
          label="删除"
          onClick={() => actions.onRemoveRoot(library, root)}
        >
          删除
        </MenuButton>
      )}
    </ActionMenu>
  )
}

function ActionMenu({ label, children }: { label: string; children: ReactNode }) {
  return (
    <details className="group relative inline-flex justify-end">
      <summary
        className="flex h-8 w-8 cursor-pointer list-none items-center justify-center rounded-lg border border-gray-200 bg-white text-ink-50 transition hover:border-primary-400/50 hover:text-brand-500 [&::-webkit-details-marker]:hidden"
        title={label}
      >
        <MoreVertical size={16} />
      </summary>
      <div className="absolute right-0 top-9 z-30 min-w-28 rounded-lg border border-gray-200 bg-white p-1 shadow-lg">
        {children}
      </div>
    </details>
  )
}

function MenuButton({
  icon,
  label,
  danger,
  onClick,
  children,
}: {
  icon: ReactNode
  label: string
  danger?: boolean
  onClick: () => void
  children: ReactNode
}) {
  const handleClick = (event: MouseEvent<HTMLButtonElement>) => {
    event.currentTarget.closest('details')?.removeAttribute('open')
    onClick()
  }
  return (
    <button
      className={`flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs transition ${
        danger ? 'text-red-500 hover:bg-red-50' : 'text-ink-100 hover:bg-gray-50 hover:text-brand-500'
      }`}
      title={label}
      onClick={handleClick}
    >
      {icon}
      <span>{children}</span>
    </button>
  )
}
