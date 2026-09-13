import { FormEvent } from 'react'
import { FolderOpen, FolderPlus, Plus, Trash2 } from 'lucide-react'

import { ModalShell } from '../components/ModalShell'
import { Select } from '../components/Select'
import type { RootDraft } from './adminLibraryPanelModel'

type CreateDialogProps = {
  name: string
  type: string
  roots: RootDraft[]
  onNameChange: (value: string) => void
  onTypeChange: (value: string) => void
  onRootChange: (index: number, patch: Partial<RootDraft>) => void
  onAddRoot: () => void
  onRemoveRoot: (index: number) => void
  onSubmit: (e: FormEvent) => void
  onClose: () => void
}

export function AdminLibraryCreateDialog({
  name,
  type,
  roots,
  onNameChange,
  onTypeChange,
  onRootChange,
  onAddRoot,
  onRemoveRoot,
  onSubmit,
  onClose,
}: CreateDialogProps) {
  return (
    <ModalShell onClose={onClose} maxWidth="max-w-xl" ariaLabel="新建媒体库" className="flex max-h-[85vh] flex-col">
      <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col">
        <div className="modal-header">
          <div className="flex items-center gap-3">
            <div className="modal-icon">
              <FolderPlus size={20} />
            </div>
            <div>
              <h3 className="font-display text-lg font-bold text-ink-600">新建媒体库</h3>
              <p className="mt-0.5 text-xs text-ink-50">
                名称和类型与现有媒体库一致时，会自动把路径追加到该媒体库。
              </p>
            </div>
          </div>
        </div>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="input-label">名称</label>
              <input
                required
                autoFocus
                className="input-base"
                placeholder="例如：电影"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
              />
            </div>
            <div>
              <label className="input-label">类型</label>
              <Select className="input-base" value={type} onChange={onTypeChange}>
                <option value="movie">电影</option>
                <option value="tv">电视剧</option>
                <option value="variety">综艺</option>
                <option value="anime">动漫</option>
                <option value="music">音乐</option>
                <option value="nfo_movie">非常规电影</option>
                <option value="nfo_tv">非常规剧集</option>
                <option value="hongguo">红果短剧（电影 / 剧集）</option>
              </Select>
            </div>
          </div>

          <div className="space-y-2">
            <label className="input-label !mb-0">入库路径</label>
            {roots.map((root, index) => (
              <CreateRootRow
                key={index}
                root={root}
                index={index}
                canRemove={roots.length > 1}
                onChange={onRootChange}
                onRemove={onRemoveRoot}
              />
            ))}
            <button
              type="button"
              className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-gray-300 px-3 py-2.5 text-sm font-semibold text-ink-50 transition hover:border-brand-400 hover:text-brand-500"
              onClick={onAddRoot}
            >
              <Plus size={15} /> 添加路径
            </button>
          </div>

          <p className="rounded-xl border border-brand-200/50 bg-brand-50 px-3 py-2 text-xs text-ink-50">
            Docker 部署请优先填写容器内路径，例如 /media/电影、/media/电视剧/国产剧。
          </p>
        </div>

        <div className="modal-footer">
          <button type="button" className="btn-outline px-4 py-2 shadow-none" onClick={onClose}>
            取消
          </button>
          <button type="submit" className="btn-primary px-4 py-2">
            <FolderPlus size={15} />
            新建 / 追加路径
          </button>
        </div>
      </form>
    </ModalShell>
  )
}

type CreateRootRowProps = {
  root: RootDraft
  index: number
  canRemove: boolean
  onChange: (index: number, patch: Partial<RootDraft>) => void
  onRemove: (index: number) => void
}

function CreateRootRow({ root, index, canRemove, onChange, onRemove }: CreateRootRowProps) {
  return (
    <div className="flex items-center gap-2 rounded-xl border border-gray-200/80 bg-gray-50/60 p-2.5">
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-gray-200 bg-white/80 text-ink-50">
        <FolderOpen size={15} />
      </div>
      <LibraryRootPathField root={root} pathRequired={index === 0} onChange={(patch) => onChange(index, patch)} />
      <button
        type="button"
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-red-300/60 text-red-400 transition hover:bg-red-50 disabled:opacity-40"
        disabled={!canRemove}
        onClick={() => onRemove(index)}
        title="删除路径"
        aria-label="删除路径"
      >
        <Trash2 size={15} />
      </button>
    </div>
  )
}

export function LibraryRootPathField({
  root,
  pathRequired = false,
  onChange,
}: {
  root: RootDraft
  pathRequired?: boolean
  onChange: (patch: Partial<RootDraft>) => void
}) {
  return (
    <div className="min-w-0 flex-1">
      <input
        required={pathRequired}
        className="input-base !py-2 font-mono"
        aria-label="路径"
        placeholder="容器路径，如 /media/电视剧/国产剧"
        value={root.path}
        onChange={(e) => onChange({ path: e.target.value })}
      />
    </div>
  )
}
