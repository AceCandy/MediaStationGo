import { useEffect, useRef } from 'react'
import { Link, Outlet } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import clsx from 'clsx'

import { LayoutSidebarContent, type LayoutSidebarContentProps } from './LayoutSidebarContent'
import { RouteErrorBoundary } from './RouteErrorBoundary'
import { VIEWER_NAV_ITEMS } from './layoutNavigation'
import type { useLayoutSidebar } from './useLayoutSidebar'

type LayoutSidebarState = ReturnType<typeof useLayoutSidebar>

type LayoutSidebarProps = {
  children: React.ReactNode
  isSidebarOpen: boolean
}

type LayoutMobileSidebarProps = {
  children: React.ReactNode
  isOpen: boolean
  onClose: () => void
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

export { LayoutHeader } from './LayoutHeaderSections'

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
  const panelRef = useRef<HTMLDivElement>(null)
  const onCloseRef = useRef(onClose)

  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  useEffect(() => {
    if (!isOpen) return undefined
    const returnTarget = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const panel = panelRef.current
    const focusable = () => Array.from(
      panel?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? [],
    )
    focusable()[0]?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab') return
      const targets = focusable()
      if (targets.length === 0) return
      const first = targets[0]
      const last = targets[targets.length - 1]
      const focusInside = panel?.contains(document.activeElement)
      if (!focusInside || (event.shiftKey && document.activeElement === first)) {
        event.preventDefault()
        ;(event.shiftKey ? last : first).focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      returnTarget?.focus()
    }
  }, [isOpen])

  return (
    <AnimatePresence>
      {isOpen && (
        <div className="fixed inset-0 z-50 flex lg:hidden">
          <motion.button
            type="button"
            aria-label="关闭导航"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="fixed inset-0 bg-black/50 backdrop-blur-md"
          />
          <motion.div
            ref={panelRef}
            initial={{ x: '-100%' }}
            animate={{ x: 0 }}
            exit={{ x: '-100%' }}
            transition={{ type: 'spring', damping: 25, stiffness: 220 }}
            className="relative z-10 flex h-full w-80 max-w-[calc(100vw-3rem)] flex-col overscroll-contain shadow-xl"
            role="dialog"
            aria-modal="true"
            aria-label="应用导航"
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

export function LayoutWorkspace({ routeKey }: LayoutWorkspaceProps) {
  return (
    <main id="main-content" tabIndex={-1} className="flex-1 overflow-y-auto px-4 py-6 md:px-8 md:py-8">
      <div className="mx-auto max-w-[1500px]">
        <AnimatePresence mode="wait">
          <motion.div
            key={routeKey}
            initial={{ opacity: 0, y: 14 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.32, ease: [0.21, 0.47, 0.32, 0.98] }}
          >
            <RouteErrorBoundary>
              <Outlet />
            </RouteErrorBoundary>
          </motion.div>
        </AnimatePresence>
      </div>
    </main>
  )
}

export function LayoutMobileBottomNav({
  pathname,
  can,
}: {
  pathname: string
  can: (key: string) => boolean
}) {
  const items = VIEWER_NAV_ITEMS.filter((item) => !item.permission || can(item.permission))
  return (
    <nav
      aria-label="主要导航"
      className="z-40 shrink-0 px-3 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] lg:hidden"
    >
      <div
        className="grid rounded-2xl border border-[var(--app-glass-border)] bg-[var(--app-glass)] p-1.5 shadow-elevated backdrop-blur-xl"
        style={{ gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))` }}
      >
        {items.map((item) => {
          const Icon = item.icon
          const active = routeMatches(pathname, item.activePaths)
          return (
            <Link
              key={item.to}
              to={item.to!}
              aria-current={active ? 'page' : undefined}
              className={clsx(
                'flex min-h-14 min-w-0 flex-col items-center justify-center gap-1 rounded-xl px-1 text-[11px] font-bold transition-all duration-300',
                active
                  ? 'text-white shadow-glow-sm'
                  : 'text-[var(--app-muted)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)]',
              )}
              style={active ? { background: 'linear-gradient(135deg, #8b5cf6 0%, #7c3aed 60%, #6d28d9 100%)' } : undefined}
            >
              <Icon size={19} aria-hidden="true" />
              <span className="truncate">{item.label}</span>
            </Link>
          )
        })}
      </div>
    </nav>
  )
}

function routeMatches(pathname: string, paths: string[]) {
  return paths.some((path) =>
    path === '/' ? pathname === '/' : pathname === path || pathname.startsWith(`${path}/`),
  )
}

export { LayoutSidebarContent }
