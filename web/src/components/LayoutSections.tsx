import { Link, Outlet } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import { Menu, MessageSquareText, Search, Sparkles } from 'lucide-react'
import clsx from 'clsx'

import type { PlayProfile, User } from '../types'
import { AppFooter } from './AppFooter'
import { LayoutSearchBox } from './LayoutSearchBox'
import { LayoutSidebarContent, type LayoutSidebarContentProps } from './LayoutSidebarContent'
import { LayoutThemeToggle } from './LayoutThemeToggle'
import { LayoutUserMenu } from './LayoutUserMenu'
import type { ThemeMode } from './useThemeMode'
import type { useLayoutSearch } from './useLayoutSearch'
import type { useLayoutProfiles } from './useLayoutProfiles'
import type { useLayoutSidebar } from './useLayoutSidebar'
import type { useThemeMode } from './useThemeMode'

type LayoutSearchState = ReturnType<typeof useLayoutSearch>
type LayoutProfileState = ReturnType<typeof useLayoutProfiles>
type LayoutSidebarState = ReturnType<typeof useLayoutSidebar>
type LayoutThemeState = ReturnType<typeof useThemeMode>

type LayoutPermissionState = {
  can: (key: string) => boolean
  isAdmin: boolean
}

type LayoutSidebarProps = {
  children: React.ReactNode
  isSidebarOpen: boolean
}

type LayoutMobileSidebarProps = {
  children: React.ReactNode
  isOpen: boolean
  onClose: () => void
}

type LayoutHeaderProps = {
  search: LayoutSearchState
  permissions: LayoutPermissionState
  theme: LayoutThemeState
  onOpenMobileDrawer: () => void
  user: User | null | undefined
  activeProfileId: string | null
  profile: LayoutProfileState
  onLogout: () => void
}

type LayoutSidebarsProps = Omit<
  LayoutSidebarContentProps,
  'isSidebarOpen' | 'isMobileDrawerOpen' | 'openGroups' | 'isRouteIn' | 'onToggleGroup' | 'onToggleSidebar' | 'onCloseMobileDrawer'
> & {
  sidebar: LayoutSidebarState
}

type LayoutWorkspaceProps = {
  routeKey: string
}

export function LayoutDesktopSidebar({ children, isSidebarOpen }: LayoutSidebarProps) {
  return (
    <aside
      className={clsx(
        'hidden lg:flex flex-col h-full shrink-0 transition-all duration-300 ease-out',
        isSidebarOpen ? 'w-64' : 'w-20',
      )}
    >
      {children}
    </aside>
  )
}

export function LayoutMobileSidebar({ children, isOpen, onClose }: LayoutMobileSidebarProps) {
  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex lg:hidden">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="fixed inset-0 bg-black/15 backdrop-blur-sm"
          />
          <motion.div
            initial={{ x: '-100%' }}
            animate={{ x: 0 }}
            exit={{ x: '-100%' }}
            transition={{ type: 'spring', damping: 25, stiffness: 220 }}
            className="relative flex w-64 max-w-xs flex-col h-full z-10 shadow-xl"
          >
            {children}
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  )
}

export function LayoutSidebars({
  sidebar,
  isAdmin,
  username,
  can,
  onLogout,
}: LayoutSidebarsProps) {
  const content = (
    <LayoutSidebarContent
      isSidebarOpen={sidebar.isSidebarOpen}
      isMobileDrawerOpen={sidebar.isMobileDrawerOpen}
      openGroups={sidebar.openGroups}
      isAdmin={isAdmin}
      username={username}
      can={can}
      isRouteIn={sidebar.isRouteIn}
      onToggleGroup={sidebar.toggleGroup}
      onToggleSidebar={() => sidebar.setIsSidebarOpen((current) => !current)}
      onCloseMobileDrawer={() => sidebar.setIsMobileDrawerOpen(false)}
      onLogout={onLogout}
    />
  )

  return (
    <>
      <LayoutDesktopSidebar isSidebarOpen={sidebar.isSidebarOpen}>{content}</LayoutDesktopSidebar>
      <LayoutMobileSidebar
        isOpen={sidebar.isMobileDrawerOpen}
        onClose={() => sidebar.setIsMobileDrawerOpen(false)}
      >
        {content}
      </LayoutMobileSidebar>
    </>
  )
}

export function LayoutHeader({
  search,
  permissions,
  theme,
  onOpenMobileDrawer,
  user,
  activeProfileId,
  profile,
  onLogout,
}: LayoutHeaderProps) {
  return (
    <header className="flex h-20 shrink-0 items-center justify-between border-b border-[var(--app-border)] bg-[var(--app-header-bg)] px-4 backdrop-blur-md z-30 md:px-8">
      <LayoutHeaderSearch search={search} onOpenMobileDrawer={onOpenMobileDrawer} />
      <LayoutHeaderActions
        permissions={permissions}
        themeMode={theme.mode}
        onThemeChange={theme.setMode}
        user={user}
        isProfileOpen={profile.isProfileOpen}
        profiles={profile.profiles}
        activeProfileId={activeProfileId}
        activeProfile={profile.activeProfile}
        onToggleProfile={() => profile.setIsProfileOpen((open) => !open)}
        onCloseProfile={() => profile.setIsProfileOpen(false)}
        onUseDefaultProfile={profile.useDefaultProfile}
        onSwitchProfile={profile.switchProfile}
        onLogout={onLogout}
      />
    </header>
  )
}

export function LayoutWorkspace({ routeKey }: LayoutWorkspaceProps) {
  return (
    <main className="flex-1 overflow-y-auto px-4 py-6 md:px-8 md:py-10">
      <div className="max-w-7xl mx-auto">
        <AnimatePresence mode="wait">
          <motion.div
            key={routeKey}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
          >
            <Outlet />
          </motion.div>
        </AnimatePresence>
      </div>
    </main>
  )
}

export function LayoutFrameFooter() {
  return (
    <AppFooter className="border-t border-[var(--app-border)] bg-[var(--app-panel)] py-5 text-center text-xs text-[var(--app-muted)]" />
  )
}

export { LayoutSidebarContent }

function LayoutHeaderSearch({
  search,
  onOpenMobileDrawer,
}: {
  search: LayoutSearchState
  onOpenMobileDrawer: () => void
}) {
  return (
    <div className="flex items-center gap-3 flex-1 max-w-lg md:gap-4">
      <button
        onClick={onOpenMobileDrawer}
        className="rounded-xl border border-[var(--app-border)] p-2.5 text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] transition-colors lg:hidden"
      >
        <Menu size={18} />
      </button>
      <LayoutSearchBox
        query={search.query}
        focused={search.focused}
        loading={search.loading}
        error={search.error}
        cards={search.cards}
        total={search.total}
        onQueryChange={search.setQuery}
        onFocusedChange={search.setFocused}
        onSubmit={search.submit}
      />
    </div>
  )
}

type LayoutHeaderActionsProps = {
  permissions: LayoutPermissionState
  themeMode: ThemeMode
  onThemeChange: (mode: ThemeMode) => void
  user: User | null | undefined
  isProfileOpen: boolean
  profiles: PlayProfile[]
  activeProfileId: string | null
  activeProfile: PlayProfile | null
  onToggleProfile: () => void
  onCloseProfile: () => void
  onUseDefaultProfile: () => void
  onSwitchProfile: (profile: PlayProfile) => void
  onLogout: () => void
}

function LayoutHeaderActions({
  permissions,
  themeMode,
  onThemeChange,
  user,
  isProfileOpen,
  profiles,
  activeProfileId,
  activeProfile,
  onToggleProfile,
  onCloseProfile,
  onUseDefaultProfile,
  onSwitchProfile,
  onLogout,
}: LayoutHeaderActionsProps) {
  return (
    <div className="flex shrink-0 items-center gap-2 sm:gap-3 md:gap-4">
      <LayoutQuickActions permissions={permissions} />
      <LayoutThemeToggle mode={themeMode} onChange={onThemeChange} />
      <span className="hidden h-6 w-px bg-[var(--app-border)] sm:block" />
      <LayoutProfileMenu
        user={user}
        isProfileOpen={isProfileOpen}
        profiles={profiles}
        activeProfileId={activeProfileId}
        activeProfile={activeProfile}
        onToggleProfile={onToggleProfile}
        onCloseProfile={onCloseProfile}
        onUseDefaultProfile={onUseDefaultProfile}
        onSwitchProfile={onSwitchProfile}
        onLogout={onLogout}
      />
    </div>
  )
}

function LayoutQuickActions({ permissions }: { permissions: LayoutPermissionState }) {
  return (
    <>
      <Link
        to="/search"
        className="rounded-xl border border-[var(--app-border)] p-2.5 text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] transition-colors sm:hidden"
      >
        <Search size={18} />
      </Link>
      {permissions.can('can_view_discover') && (
        <Link
          to="/discover"
          className="hidden md:flex items-center gap-2 rounded-xl border border-[var(--app-border)] px-4 py-2.5 text-xs font-bold text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] transition-all"
        >
          <Sparkles size={14} className="text-brand-500" />
          <span>发现新片</span>
        </Link>
      )}
      {permissions.isAdmin && (
        <Link
          to="/notify-channels"
          title="通知配置"
          aria-label="打开通知配置"
          className="relative rounded-xl border border-[var(--app-border)] p-2.5 text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] transition-all"
        >
          <MessageSquareText size={18} />
        </Link>
      )}
    </>
  )
}

function LayoutProfileMenu({
  user,
  isProfileOpen,
  profiles,
  activeProfileId,
  activeProfile,
  onToggleProfile,
  onCloseProfile,
  onUseDefaultProfile,
  onSwitchProfile,
  onLogout,
}: Omit<LayoutHeaderActionsProps, 'permissions' | 'themeMode' | 'onThemeChange'>) {
  return (
    <LayoutUserMenu
      user={user}
      isOpen={isProfileOpen}
      profiles={profiles}
      activeProfileId={activeProfileId}
      activeProfile={activeProfile}
      onToggle={onToggleProfile}
      onClose={onCloseProfile}
      onUseDefaultProfile={onUseDefaultProfile}
      onSwitchProfile={onSwitchProfile}
      onLogout={onLogout}
    />
  )
}
