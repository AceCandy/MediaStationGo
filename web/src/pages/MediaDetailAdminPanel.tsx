import { Database, FolderInput, MoreHorizontal, Pencil, Search, Sparkles, Trash2, type LucideIcon } from 'lucide-react'

type MediaDetailAdminMenuProps = {
  onSmartScrape: () => void
  onManualScrape: () => void
  onMetadataEdit: () => void
  onOrganize: () => void
  onProbe: () => void
  onSoftDelete: () => void
}

// MediaDetailAdminMenu 管理操作收敛进播放操作排的「更多操作」下拉菜单。
export function MediaDetailAdminMenu({
  onSmartScrape,
  onManualScrape,
  onMetadataEdit,
  onOrganize,
  onProbe,
  onSoftDelete,
}: MediaDetailAdminMenuProps) {
  const close = (el: HTMLElement) => {
    const details = el.closest('details')
    if (details) details.open = false
  }

  return (
    <details
      className="group relative"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Escape') return
        event.preventDefault()
        event.currentTarget.open = false
        event.currentTarget.querySelector('summary')?.focus()
      }}
    >
      <summary
        className="btn-outline cursor-pointer list-none [&::-webkit-details-marker]:hidden"
        aria-label="更多操作"
      >
        <MoreHorizontal size={16} />
        <span>更多操作</span>
      </summary>

      <div
        role="menu"
        aria-label="管理操作"
        className="absolute right-0 top-full z-50 mt-2 w-64 overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-1.5 shadow-xl"
      >
        <AdminMenuItem icon={Sparkles} iconClass="text-[var(--app-gold)]" label="智能刮削 (TMDB)" onClick={onSmartScrape} onClose={close} />
        <AdminMenuItem icon={Search} iconClass="text-[var(--app-gold)]" label="手动匹配刮削" onClick={onManualScrape} onClose={close} />
        <AdminMenuItem icon={Pencil} iconClass="text-[var(--app-muted)]" label="编辑元数据" onClick={onMetadataEdit} onClose={close} />
        <AdminMenuItem icon={FolderInput} iconClass="text-[var(--app-gold)]" label="整理入库" onClick={onOrganize} onClose={close} />
        <AdminMenuItem icon={Database} iconClass="text-[var(--app-muted)]" label="强制探测媒体轨 (ffprobe)" onClick={onProbe} onClose={close} />
        <div className="mx-2 my-1.5 border-t border-[var(--app-border)]" />
        <AdminMenuItem icon={Trash2} iconClass="text-red-500" label="永久删除" danger onClick={onSoftDelete} onClose={close} />
      </div>
    </details>
  )
}

function AdminMenuItem({
  icon: Icon,
  iconClass,
  label,
  danger = false,
  onClick,
  onClose,
}: {
  icon: LucideIcon
  iconClass: string
  label: string
  danger?: boolean
  onClick: () => void
  onClose: (el: HTMLElement) => void
}) {
  return (
    <button
      type="button"
      role="menuitem"
      className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left text-sm font-semibold transition-colors ${
        danger ? 'text-red-500 hover:bg-red-500/10' : 'text-[var(--app-text)] hover:bg-[var(--app-hover)]'
      }`}
      onClick={(event) => {
        onClick()
        onClose(event.currentTarget)
      }}
    >
      <Icon size={15} className={`shrink-0 ${iconClass}`} />
      <span className="min-w-0 flex-1 truncate">{label}</span>
    </button>
  )
}
