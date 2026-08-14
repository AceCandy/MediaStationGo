import {
  HardDrive,
  Home,
  Library,
  ListChecks,
  Plug,
  Settings,
  SlidersHorizontal,
  type LucideIcon,
} from 'lucide-react'

import { resolveAppRoutes, type ManagementGroupID, type NavigationScope } from '../appRoutes'

export type LayoutNavGroupID = 'viewer' | 'management'

export type LayoutNavItem = {
  to?: string
  label: string
  icon: LucideIcon
  activePaths: string[]
  group?: ManagementGroupID
  children?: LayoutNavItem[]
  end?: boolean
  permission?: string
  adminOnly?: boolean
}

export type LayoutNavGroup = {
  id: LayoutNavGroupID
  label: string
  icon: LucideIcon
  activePaths: string[]
  adminOnly?: boolean
  items: LayoutNavItem[]
}

type ManagementGroupDefinition = {
  group: ManagementGroupID
  label: string
  icon: LucideIcon
  activePaths: string[]
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
        group: navigation.group,
        end: navigation.end,
        permission,
        adminOnly,
      }
    })
}

export const VIEWER_NAV_ITEMS = navigationItems('viewer')
const ALL_MANAGEMENT_NAV_ITEMS = navigationItems('management')
const MANAGEMENT_GROUPS: ManagementGroupDefinition[] = [
  { group: 'media', label: '媒体入库', icon: Library, activePaths: ['/admin/media'] },
  { group: 'storage', label: '存储与清理', icon: HardDrive, activePaths: ['/admin/storage'] },
  { group: 'tasks', label: '任务与运行', icon: ListChecks, activePaths: ['/admin/tasks'] },
  { group: 'integrations', label: '用户与集成', icon: Plug, activePaths: ['/admin/integrations'] },
  { group: 'settings', label: '系统设置', icon: SlidersHorizontal, activePaths: ['/admin/settings'] },
]
const MANAGEMENT_NAV_ITEMS: LayoutNavItem[] = MANAGEMENT_GROUPS.map((group) => ({
  ...group,
  children: ALL_MANAGEMENT_NAV_ITEMS.filter((item) => item.group === group.group),
}))

export const LAYOUT_NAV_GROUPS: LayoutNavGroup[] = [
  {
    id: 'viewer',
    label: '观看空间',
    icon: Home,
    activePaths: VIEWER_NAV_ITEMS.flatMap((item) => item.activePaths),
    items: VIEWER_NAV_ITEMS,
  },
  {
    id: 'management',
    label: '管理空间',
    icon: Settings,
    activePaths: MANAGEMENT_NAV_ITEMS.flatMap((item) => item.activePaths),
    adminOnly: true,
    items: MANAGEMENT_NAV_ITEMS,
  },
]

export const NAV_GROUP_PATHS: Record<string, string[]> = {
  ...Object.fromEntries(LAYOUT_NAV_GROUPS.map((group) => [group.id, group.activePaths])),
  ...Object.fromEntries(MANAGEMENT_GROUPS.map((group) => [group.group, group.activePaths])),
}
