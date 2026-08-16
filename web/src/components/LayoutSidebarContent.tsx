import { useId } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { LogOut, Menu, X } from 'lucide-react'
import clsx from 'clsx'
import { LAYOUT_NAV_GROUPS, type LayoutNavGroup, type LayoutNavItem } from './layoutNavigation'
import { SidebarGroup, SidebarLink } from './LayoutSidebarNav'

export type LayoutSidebarContentProps = {
  isSidebarOpen: boolean
  isMobileDrawerOpen: boolean
  openGroups: Record<string, boolean>
  isAdmin: boolean
  username?: string
  can: (key: string) => boolean
  isRouteIn: (paths: string[], end?: boolean) => boolean
  onToggleGroup: (id: string) => void
  onToggleSidebar: () => void
  onCloseMobileDrawer: () => void
  onLogout: () => void
}

type VisibleLayoutNavGroup = {
  group: LayoutNavGroup
  items: LayoutNavItem[]
}

export function LayoutSidebarContent({
  isSidebarOpen,
  isMobileDrawerOpen,
  openGroups,
  isAdmin,
  username,
  can,
  isRouteIn,
  onToggleGroup,
  onToggleSidebar,
  onCloseMobileDrawer,
  onLogout,
}: LayoutSidebarContentProps) {
  const sidebarExpanded = isSidebarOpen || isMobileDrawerOpen
  const visibleGroups = visibleSidebarGroups({ isAdmin, can })
  const navigationId = useId()

  return (
    <div className="flex h-full flex-col border-r border-[var(--app-border)] bg-[var(--app-glass)] backdrop-blur-xl">
      <LayoutSidebarHeader
        sidebarExpanded={sidebarExpanded}
        navigationId={navigationId}
        onToggleSidebar={onToggleSidebar}
        onCloseMobileDrawer={onCloseMobileDrawer}
      />
      <LayoutSidebarNav
        groups={visibleGroups}
        navigationId={navigationId}
        sidebarExpanded={sidebarExpanded}
        openGroups={openGroups}
        isRouteIn={isRouteIn}
        onToggleGroup={onToggleGroup}
      />
      <LayoutSidebarLogout sidebarExpanded={sidebarExpanded} username={username} onLogout={onLogout} />
    </div>
  )
}

function visibleSidebarGroups({
  isAdmin,
  can,
}: Pick<LayoutSidebarContentProps, 'isAdmin' | 'can'>): VisibleLayoutNavGroup[] {
  const isItemVisible = (item: LayoutNavItem) =>
    (!item.adminOnly || isAdmin) && (!item.permission || can(item.permission))
  return LAYOUT_NAV_GROUPS
    .filter((group) => !group.adminOnly || isAdmin)
    .map((group) => ({
      group,
      items: group.items.filter(isItemVisible),
    }))
    .filter(({ items }) => items.length > 0)
}

function LayoutSidebarHeader({
  sidebarExpanded,
  navigationId,
  onToggleSidebar,
  onCloseMobileDrawer,
}: {
  sidebarExpanded: boolean
  navigationId: string
  onToggleSidebar: () => void
  onCloseMobileDrawer: () => void
}) {
  return (
    <div className="flex h-16 items-center justify-between border-b border-[var(--app-border)] px-4">
      <Link to="/" className="group flex min-w-0 items-center gap-2.5">
        <img
          src="/brand/mediastationgo-logo.svg"
          alt="MediaStationGo"
          className="h-9 w-9 shrink-0 rounded-xl object-contain shadow-glow-sm transition-transform duration-300 group-hover:scale-105"
        />
        {sidebarExpanded && (
          <motion.span
            initial={{ opacity: 0, x: -10 }}
            animate={{ opacity: 1, x: 0 }}
            className="truncate font-display text-sm font-extrabold tracking-tight text-[var(--app-text)]"
          >
            Media<span className="text-gradient-brand">Station</span>Go
          </motion.span>
        )}
      </Link>
      <SidebarIconButton
        label={sidebarExpanded ? '折叠侧栏' : '展开侧栏'}
        className="hidden lg:block"
        expanded={sidebarExpanded}
        controls={navigationId}
        onClick={onToggleSidebar}
      >
        <Menu size={18} />
      </SidebarIconButton>
      <SidebarIconButton
        label="关闭导航"
        className="block lg:hidden"
        expanded
        controls={navigationId}
        onClick={onCloseMobileDrawer}
      >
        <X size={18} />
      </SidebarIconButton>
    </div>
  )
}

function LayoutSidebarNav({
  groups,
  navigationId,
  sidebarExpanded,
  openGroups,
  isRouteIn,
  onToggleGroup,
}: {
  groups: VisibleLayoutNavGroup[]
  navigationId: string
  sidebarExpanded: boolean
  openGroups: Record<string, boolean>
  isRouteIn: (paths: string[], end?: boolean) => boolean
  onToggleGroup: (id: string) => void
}) {
  return (
    <nav id={navigationId} aria-label="侧栏导航" className="flex-1 overflow-y-auto px-3 py-4 space-y-1.5 scrollbar-hide">
      {groups.map(({ group, items }) => (
        <LayoutSidebarNavGroup
          key={group.id}
          group={group}
          items={items}
          sidebarExpanded={sidebarExpanded}
          open={openGroups[group.id] ?? false}
          active={items.some((item) => isRouteIn(item.activePaths, item.end))}
          isRouteIn={isRouteIn}
          onToggleGroup={onToggleGroup}
        />
      ))}
    </nav>
  )
}

function LayoutSidebarNavGroup({
  group,
  items,
  sidebarExpanded,
  open,
  active,
  isRouteIn,
  onToggleGroup,
}: {
  group: LayoutNavGroup
  items: LayoutNavItem[]
  sidebarExpanded: boolean
  open: boolean
  active: boolean
  isRouteIn: (paths: string[], end?: boolean) => boolean
  onToggleGroup: (id: string) => void
}) {
  const GroupIcon = group.icon
  return (
    <SidebarGroup
      id={group.id}
      icon={<GroupIcon size={18} />}
      label={group.label}
      collapsed={!sidebarExpanded}
      open={open}
      active={active}
      onToggle={onToggleGroup}
    >
      {items.map((item) => {
        const ItemIcon = item.icon
        return (
          <SidebarLink
            key={item.to}
            to={item.to!}
            icon={<ItemIcon size={16} />}
            label={item.label}
            end={item.end}
            active={isRouteIn(item.activePaths, item.end)}
            child
          />
        )
      })}
    </SidebarGroup>
  )
}

function LayoutSidebarLogout({
  sidebarExpanded,
  username,
  onLogout,
}: {
  sidebarExpanded: boolean
  username?: string
  onLogout: () => void
}) {
  return (
    <div className="border-t border-[var(--app-border)] p-3">
      <button
        onClick={onLogout}
        className={clsx(
          'flex items-center gap-3 rounded-xl px-3.5 py-2.5 text-[13px] font-semibold transition-all duration-300 w-full group/logout',
          sidebarExpanded
            ? 'justify-start text-[var(--app-muted)] hover:bg-[var(--app-danger-soft)] hover:text-red-500'
            : 'justify-center text-[var(--app-muted)] hover:text-red-500',
        )}
        title={`安全登出 (${username ?? ''})`}
      >
        <LogOut size={17} className="transition-transform group-hover/logout:-translate-x-0.5" />
        {sidebarExpanded && <span>安全退出</span>}
      </button>
    </div>
  )
}

function SidebarIconButton({
  children,
  className,
  label,
  expanded,
  controls,
  onClick,
}: {
  children: React.ReactNode
  className: string
  label: string
  expanded?: boolean
  controls?: string
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      aria-expanded={expanded}
      aria-controls={controls}
      title={label}
      className={clsx(
        'min-h-11 min-w-11 rounded-xl p-2.5 text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] transition-colors',
        className,
      )}
    >
      {children}
    </button>
  )
}
