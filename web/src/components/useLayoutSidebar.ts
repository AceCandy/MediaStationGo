import { useCallback, useEffect, useState } from 'react'

import { LAYOUT_NAV_GROUPS } from './layoutNavigation'

export function useLayoutSidebar(pathname: string) {
  const [isSidebarOpen, setIsSidebarOpen] = useState(true)
  const [isMobileDrawerOpen, setIsMobileDrawerOpen] = useState(false)
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({ viewer: true })

  useEffect(() => {
    const handleResize = () => {
      setIsSidebarOpen(window.innerWidth >= 1024)
    }
    handleResize()
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  useEffect(() => {
    setIsMobileDrawerOpen(false)
  }, [pathname])

  const isRouteIn = useCallback(
    (paths: string[], end = false) =>
      paths.some((path) =>
        path === '/' || end
          ? pathname === path
          : pathname === path || pathname.startsWith(`${path}/`),
      ),
    [pathname],
  )

  const toggleGroup = useCallback(
    (key: string) => setOpenGroups((current) => ({ ...current, [key]: !current[key] })),
    [],
  )

  useEffect(() => {
    const active = LAYOUT_NAV_GROUPS
      .filter((group) => group.items.some((item) => isRouteIn(item.activePaths, item.end)))
      .map((group) => [group.id, true])
    if (active.length > 0) setOpenGroups(Object.fromEntries(active))
  }, [isRouteIn])

  return {
    isMobileDrawerOpen,
    isRouteIn,
    isSidebarOpen,
    openGroups,
    setIsMobileDrawerOpen,
    setIsSidebarOpen,
    toggleGroup,
  }
}
