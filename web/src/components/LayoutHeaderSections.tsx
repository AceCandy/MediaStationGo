import { NavLink } from 'react-router-dom'
import { Menu } from 'lucide-react'

import type { PlayProfile, User } from '../types'
import { HEADER_NAV_ITEMS } from './layoutNavigation'
import { LayoutSearchBox } from './LayoutSearchBox'
import { LayoutThemeToggle } from './LayoutThemeToggle'
import { LayoutUserMenu } from './LayoutUserMenu'
import type { useLayoutProfiles } from './useLayoutProfiles'
import type { useLayoutSearch } from './useLayoutSearch'
import type { ThemeMode, useThemeMode } from './useThemeMode'

type LayoutSearchState = ReturnType<typeof useLayoutSearch>
type LayoutProfileState = ReturnType<typeof useLayoutProfiles>
type LayoutThemeState = ReturnType<typeof useThemeMode>

type LayoutPermissionState = {
  can: (key: string) => boolean
  isAdmin: boolean
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
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center justify-between border-b border-[var(--app-border)] bg-[var(--app-header-bg)] px-4 backdrop-blur-xl md:px-8">
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

function LayoutHeaderSearch({
  search,
  onOpenMobileDrawer,
}: {
  search: LayoutSearchState
  onOpenMobileDrawer: () => void
}) {
  return (
    <div className="flex min-w-0 items-center gap-2 flex-1 max-w-lg mr-2 md:gap-4">
      <button
        onClick={onOpenMobileDrawer}
        aria-label="打开导航"
        title="打开导航"
        className="min-h-11 min-w-11 rounded-xl p-2.5 text-[var(--app-muted)] transition-all duration-300 hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] lg:hidden"
      >
        <Menu size={18} />
      </button>
      <LayoutSearchBox
        aiOn={search.aiOn}
        aiAvailable={search.aiAvailable}
        onToggleAI={search.toggleAI}
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
      <div className="hidden xl:block">
        <LayoutThemeToggle mode={themeMode} onChange={onThemeChange} />
      </div>
      <span className="hidden h-6 w-px bg-[var(--app-border)] sm:block" />
      <LayoutProfileMenu
        user={user}
        isProfileOpen={isProfileOpen}
        profiles={profiles}
        activeProfileId={activeProfileId}
        activeProfile={activeProfile}
        themeMode={themeMode}
        onToggleProfile={onToggleProfile}
        onCloseProfile={onCloseProfile}
        onThemeChange={onThemeChange}
        onUseDefaultProfile={onUseDefaultProfile}
        onSwitchProfile={onSwitchProfile}
        onLogout={onLogout}
      />
    </div>
  )
}

function LayoutQuickActions({ permissions }: { permissions: LayoutPermissionState }) {
  const headerItems = HEADER_NAV_ITEMS.filter(
    (item) => (!item.adminOnly || permissions.isAdmin) && (!item.permission || permissions.can(item.permission)),
  )

  return (
    <>
      {headerItems.map((item) => {
        const Icon = item.icon
        return (
          <NavLink
            key={item.to}
            to={item.to!}
            end={item.end}
            title={item.label}
            aria-label={`打开${item.label}`}
            className={({ isActive }) =>
              `relative min-h-11 min-w-11 rounded-xl p-2.5 transition-all duration-300 hover:bg-[var(--app-hover)] hover:text-[var(--app-text)] ${
                isActive ? 'bg-[var(--app-brand-soft)] text-[var(--app-brand-text)]' : 'text-[var(--app-muted)]'
              }`
            }
          >
            <Icon size={18} aria-hidden="true" />
          </NavLink>
        )
      })}
    </>
  )
}

function LayoutProfileMenu({
  user,
  isProfileOpen,
  profiles,
  activeProfileId,
  activeProfile,
  themeMode,
  onToggleProfile,
  onCloseProfile,
  onThemeChange,
  onUseDefaultProfile,
  onSwitchProfile,
  onLogout,
}: Omit<LayoutHeaderActionsProps, 'permissions'>) {
  return (
    <LayoutUserMenu
      user={user}
      isOpen={isProfileOpen}
      profiles={profiles}
      activeProfileId={activeProfileId}
      activeProfile={activeProfile}
      themeMode={themeMode}
      onToggle={onToggleProfile}
      onClose={onCloseProfile}
      onThemeChange={onThemeChange}
      onUseDefaultProfile={onUseDefaultProfile}
      onSwitchProfile={onSwitchProfile}
      onLogout={onLogout}
    />
  )
}
