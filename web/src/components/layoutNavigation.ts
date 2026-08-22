import {
  Home,
  Library,
  Settings,
  type LucideIcon,
} from 'lucide-react'

import { resolveAppRoutes, type NavigationScope } from '../appRoutes'

export type LayoutNavGroupID = 'viewer' | 'files' | 'management'

export type LayoutNavItem = {
  to?: string
  label: string
  icon: LucideIcon
  activePaths: string[]
  end?: boolean
  permission?: string
  adminOnly?: boolean
}

export type LayoutNavGroup = {
  id: LayoutNavGroupID
  label: string
  icon: LucideIcon
  adminOnly?: boolean
  items: LayoutNavItem[]
}

function navigationItems(scope: NavigationScope): LayoutNavItem[] {
  return resolveAppRoutes()
    .filter(({ route }) => route.navigation?.scope === scope)
    .sort((left, right) => (left.route.navigation?.order ?? 0) - (right.route.navigation?.order ?? 0))
    .map(({ route, adminOnly, permission }) => {
      const navigation = route.navigation!
      return {
        to: navigation.to,
        label: navigation.label,
        icon: navigation.icon,
        activePaths: navigation.activePaths ?? [navigation.to],
        end: navigation.end,
        permission,
        adminOnly,
      }
    })
}

export const VIEWER_NAV_ITEMS = navigationItems('viewer')
export const HEADER_NAV_ITEMS = navigationItems('header')
const FILE_NAV_ITEMS = navigationItems('files')
const MANAGEMENT_NAV_ITEMS = navigationItems('management')

export const LAYOUT_NAV_GROUPS: LayoutNavGroup[] = [
  {
    id: 'viewer',
    label: '观看空间',
    icon: Home,
    items: VIEWER_NAV_ITEMS,
  },
  {
    id: 'files',
    label: '文件空间',
    icon: Library,
    adminOnly: true,
    items: FILE_NAV_ITEMS,
  },
  {
    id: 'management',
    label: '管理空间',
    icon: Settings,
    adminOnly: true,
    items: MANAGEMENT_NAV_ITEMS,
  },
]
