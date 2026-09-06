import { Database, MoreHorizontal, Pencil, RefreshCw, Trash2, type LucideIcon } from 'lucide-react'

type MediaDetailAdminMenuProps = {
  label?: string
  refreshLabel?: string
  disabled?: boolean
  onTMDbRefresh?: () => void
  tmdbRefreshPending: boolean
  onDoubanEnrich?: () => void
  doubanEnrichmentPending: boolean
  doubanDegraded: boolean
  onMetadataEdit: () => void
  onProbe: () => void
  onSoftDelete: () => void
}

// MediaDetailAdminMenu 管理操作收敛进播放操作排的「更多操作」下拉菜单。
export function MediaDetailAdminMenu({
  label = '更多操作',
  refreshLabel = '刷新tmdb信息',
  disabled = false,
  onTMDbRefresh,
  tmdbRefreshPending,
  onDoubanEnrich,
  doubanEnrichmentPending,
  doubanDegraded,
  onMetadataEdit,
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
        aria-label={label}
      >
        <MoreHorizontal size={16} />
        <span>{label}</span>
      </summary>

      <fieldset disabled={disabled}
        role="menu"
        aria-label="管理操作"
        className="absolute left-0 top-full z-50 mt-2 w-64 max-w-[calc(100vw-4rem)] overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-1.5 shadow-xl"
      >
        {onTMDbRefresh && <AdminMenuItem icon={RefreshCw} iconClass={`text-[var(--app-gold)] ${tmdbRefreshPending ? 'animate-spin' : ''}`} label={tmdbRefreshPending ? '处理中…' : refreshLabel} disabled={tmdbRefreshPending} onClick={onTMDbRefresh} onClose={close} />}
        {onDoubanEnrich && (
          <AdminMenuItem
            icon={RefreshCw}
            iconClass={`text-[var(--app-gold)] ${doubanEnrichmentPending ? 'animate-spin' : ''}`}
            label={doubanEnrichmentPending ? '正在补齐豆瓣信息…' : doubanDegraded ? '重试完整豆瓣信息' : '补齐豆瓣信息'}
            disabled={doubanEnrichmentPending}
            onClick={onDoubanEnrich}
            onClose={close}
          />
        )}
        <AdminMenuItem icon={Pencil} iconClass="text-[var(--app-muted)]" label="编辑元数据" onClick={onMetadataEdit} onClose={close} />
        <AdminMenuItem icon={Database} iconClass="text-[var(--app-muted)]" label="强制探测媒体轨 (ffprobe)" onClick={onProbe} onClose={close} />
        <div className="mx-2 my-1.5 border-t border-[var(--app-border)]" />
        <AdminMenuItem icon={Trash2} iconClass="text-red-500" label="永久删除" danger onClick={onSoftDelete} onClose={close} />
      </fieldset>
    </details>
  )
}

function AdminMenuItem({
  icon: Icon,
  iconClass,
  label,
  danger = false,
  disabled = false,
  onClick,
  onClose,
}: {
  icon: LucideIcon
  iconClass: string
  label: string
  danger?: boolean
  disabled?: boolean
  onClick: () => void
  onClose: (el: HTMLElement) => void
}) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left text-sm font-semibold transition-colors ${
        danger ? 'text-red-500 hover:bg-red-500/10' : 'text-[var(--app-text)] hover:bg-[var(--app-hover)]'
      } disabled:cursor-not-allowed disabled:opacity-50`}
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
