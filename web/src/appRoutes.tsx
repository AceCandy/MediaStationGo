/* eslint-disable react-refresh/only-export-components */
import { lazy, type ReactElement } from 'react'
import {
  BarChart3,
  Bell,
  Bot,
  Code2,
  Compass,
  Copy,
  FileText,
  HardDrive,
  Home,
  KeyRound,
  Library,
  ListChecks,
  SlidersHorizontal,
  Trash2,
  User,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { Navigate, Outlet, useSearchParams } from 'react-router-dom'

const HomePage = lazy(() => import('./pages/HomePage').then((m) => ({ default: m.HomePage })))
const LibraryPage = lazy(() => import('./pages/LibraryPage').then((m) => ({ default: m.LibraryPage })))
const LibrariesPage = lazy(() => import('./pages/LibrariesPage').then((m) => ({ default: m.LibrariesPage })))
const SearchPage = lazy(() => import('./pages/SearchPage').then((m) => ({ default: m.SearchPage })))
const MePage = lazy(() => import('./pages/MePage').then((m) => ({ default: m.MePage })))
const PlaylistDetailPage = lazy(() =>
  import('./pages/PlaylistDetailPage').then((m) => ({ default: m.PlaylistDetailPage })),
)
const MediaDetailPage = lazy(() => import('./pages/MediaDetailPage').then((m) => ({ default: m.MediaDetailPage })))
const PlayerPage = lazy(() => import('./pages/PlayerPage').then((m) => ({ default: m.PlayerPage })))
const AdminMediaPage = lazy(() => import('./pages/AdminPage').then((m) => ({ default: m.AdminMediaPage })))
const AdminUsersPage = lazy(() => import('./pages/AdminPage').then((m) => ({ default: m.AdminUsersPage })))
const AdminAPIsPage = lazy(() => import('./pages/AdminPage').then((m) => ({ default: m.AdminAPIsPage })))
const AdminEmbyAPIsPage = lazy(() =>
  import('./pages/AdminEmbyAPIsPage').then((m) => ({ default: m.AdminEmbyAPIsPage })),
)
const ProfilePage = lazy(() => import('./pages/ProfilePage').then((m) => ({ default: m.ProfilePage })))
const StatsPage = lazy(() => import('./pages/StatsPage').then((m) => ({ default: m.StatsPage })))
const DiscoverPage = lazy(() => import('./pages/DiscoverPage').then((m) => ({ default: m.DiscoverPage })))
const TasksPage = lazy(() => import('./pages/TasksPage').then((m) => ({ default: m.TasksPage })))
const RecycleBinPage = lazy(() => import('./pages/RecycleBinPage').then((m) => ({ default: m.RecycleBinPage })))
const DlnaPage = lazy(() => import('./pages/DlnaPage').then((m) => ({ default: m.DlnaPage })))
const FileManagerPage = lazy(() =>
  import('./pages/FileManagerPage').then((m) => ({ default: m.FileManagerPage })),
)
const StoragePage = lazy(() => import('./pages/StoragePage').then((m) => ({ default: m.StoragePage })))
const DuplicatesPage = lazy(() => import('./pages/DuplicatesPage').then((m) => ({ default: m.DuplicatesPage })))
const StrmPage = lazy(() => import('./pages/StrmPage').then((m) => ({ default: m.StrmPage })))
const ProfileManagementPage = lazy(() =>
  import('./pages/ProfileManagementPage').then((m) => ({ default: m.ProfileManagementPage })),
)
const NotifyChannelsPage = lazy(() =>
  import('./pages/NotifyChannelsPage').then((m) => ({ default: m.NotifyChannelsPage })),
)
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))
const AssistantChatPage = lazy(() =>
  import('./pages/AssistantChatPage').then((m) => ({ default: m.AssistantChatPage })),
)

export type NavigationScope = 'viewer' | 'management'
export type ManagementGroupID = 'media' | 'storage' | 'tasks' | 'integrations' | 'settings'

export type AppRouteNavigation = {
  scope: NavigationScope
  label: string
  icon: LucideIcon
  to: string
  order: number
  group?: ManagementGroupID
  activePaths?: string[]
  end?: boolean
}

export type AppRoute = {
  id: string
  path?: string
  index?: boolean
  element: ReactElement
  adminOnly?: boolean
  permission?: string
  deniedTo?: string
  navigation?: AppRouteNavigation
  children?: AppRoute[]
}

export type ResolvedAppRoute = {
  route: AppRoute
  adminOnly: boolean
  permission?: string
}

export function resolveAppRoutes(
  routes: AppRoute[] = appRoutes,
  inherited: Pick<ResolvedAppRoute, 'adminOnly' | 'permission'> = { adminOnly: false },
): ResolvedAppRoute[] {
  return routes.flatMap((route) => {
    const access = {
      adminOnly: inherited.adminOnly || Boolean(route.adminOnly),
      permission: route.permission ?? inherited.permission,
    }
    return [{ route, ...access }, ...resolveAppRoutes(route.children ?? [], access)]
  })
}

function AdminEntryPage() {
  const [searchParams] = useSearchParams()
  const tabs = searchParams.getAll('tab')
  if (tabs.length === 0) return <Navigate to="/admin/media" replace />
  if (tabs.length !== 1) return <Navigate to="/admin/media" replace />

  const target = {
    library: '/admin/media',
    users: '/admin/integrations/users',
    api: '/admin/integrations/apis',
  }[tabs[0]]
  return <Navigate to={target ?? '/admin/media'} replace />
}

export const appRoutes: AppRoute[] = [
  {
    id: 'home',
    index: true,
    element: <HomePage />,
    navigation: {
      scope: 'viewer',
      label: '首页',
      icon: Home,
      to: '/',
      order: 10,
      activePaths: ['/'],
      end: true,
    },
  },
  {
    id: 'libraries',
    path: 'libraries',
    element: <LibrariesPage />,
    navigation: {
      scope: 'viewer',
      label: '媒体库',
      icon: Library,
      to: '/libraries',
      order: 20,
      activePaths: ['/libraries', '/library', '/media', '/play'],
    },
  },
  { id: 'library-detail', path: 'library/:id', element: <LibraryPage /> },
  {
    id: 'discover',
    path: 'discover',
    element: <DiscoverPage />,
    permission: 'can_view_discover',
    navigation: {
      scope: 'viewer',
      label: '发现',
      icon: Compass,
      to: '/discover',
      order: 30,
      activePaths: ['/discover'],
    },
  },
  { id: 'search', path: 'search', element: <SearchPage /> },
  {
    id: 'me',
    path: 'me',
    element: <MePage />,
    navigation: {
      scope: 'viewer',
      label: '我的',
      icon: User,
      to: '/me?tab=favourites',
      order: 40,
      activePaths: ['/me', '/playlist'],
    },
  },
  { id: 'playlist-detail', path: 'playlist/:id', element: <PlaylistDetailPage /> },
  { id: 'media-detail', path: 'media/:id', element: <MediaDetailPage /> },
  { id: 'player', path: 'play/:id', element: <PlayerPage /> },
  { id: 'profile', path: 'profile', element: <ProfilePage /> },
  {
    id: 'dlna',
    path: 'dlna',
    element: <DlnaPage />,
    permission: 'can_cast',
    deniedTo: '/',
  },
  { id: 'play-profiles', path: 'play-profiles', element: <ProfileManagementPage /> },

  { id: 'legacy-poster-wall', path: 'poster-wall', element: <Navigate to="/libraries?view=poster" replace /> },
  { id: 'legacy-favourites', path: 'favourites', element: <Navigate to="/me?tab=favourites" replace /> },
  { id: 'legacy-playlists', path: 'playlists', element: <Navigate to="/me?tab=playlists" replace /> },
  { id: 'legacy-history', path: 'history', element: <Navigate to="/me?tab=history" replace /> },
  { id: 'legacy-ai', path: 'ai', element: <Navigate to="/search?mode=ai" replace /> },

  {
    id: 'admin-root',
    path: 'admin',
    element: <Outlet />,
    adminOnly: true,
    children: [
      { id: 'admin-overview', index: true, element: <AdminEntryPage /> },
      {
        id: 'admin-media',
        path: 'media',
        element: <AdminMediaPage />,
        navigation: { scope: 'management', group: 'media', label: '媒体库管理', icon: Library, to: '/admin/media', order: 10, end: true },
      },
      {
        id: 'admin-media-files',
        path: 'media/files',
        element: <FileManagerPage />,
        navigation: { scope: 'management', group: 'media', label: '文件与入库', icon: FileText, to: '/admin/media/files', order: 20 },
      },
      {
        id: 'admin-media-strm',
        path: 'media/strm',
        element: <StrmPage />,
        navigation: { scope: 'management', group: 'media', label: 'STRM 工具', icon: FileText, to: '/admin/media/strm', order: 30 },
      },
      {
        id: 'admin-storage',
        path: 'storage',
        element: <StoragePage />,
        navigation: { scope: 'management', group: 'storage', label: '存储概览', icon: HardDrive, to: '/admin/storage', order: 10, end: true },
      },
      {
        id: 'admin-storage-duplicates',
        path: 'storage/duplicates',
        element: <DuplicatesPage />,
        navigation: { scope: 'management', group: 'storage', label: '重复文件', icon: Copy, to: '/admin/storage/duplicates', order: 20 },
      },
      {
        id: 'admin-storage-recycle',
        path: 'storage/recycle',
        element: <RecycleBinPage />,
        navigation: { scope: 'management', group: 'storage', label: '回收站', icon: Trash2, to: '/admin/storage/recycle', order: 30 },
      },
      {
        id: 'admin-tasks',
        path: 'tasks',
        element: <TasksPage />,
        navigation: { scope: 'management', group: 'tasks', label: '任务中心', icon: ListChecks, to: '/admin/tasks', order: 10, end: true },
      },
      {
        id: 'admin-tasks-scheduler',
        path: 'tasks/scheduler',
        element: <Navigate to="/admin/tasks?panel=scheduler" replace />,
      },
      {
        id: 'admin-tasks-stats',
        path: 'tasks/stats',
        element: <StatsPage />,
        navigation: { scope: 'management', group: 'tasks', label: '运行监控', icon: BarChart3, to: '/admin/tasks/stats', order: 20 },
      },
      {
        id: 'admin-integrations',
        path: 'integrations',
        element: <Navigate to="/admin/integrations/users" replace />,
      },
      {
        id: 'admin-integrations-users',
        path: 'integrations/users',
        element: <AdminUsersPage />,
        navigation: { scope: 'management', group: 'integrations', label: '用户管理', icon: Users, to: '/admin/integrations/users', order: 20 },
      },
      {
        id: 'admin-integrations-apis',
        path: 'integrations/apis',
        element: <AdminAPIsPage />,
        navigation: { scope: 'management', group: 'integrations', label: '外部 API', icon: KeyRound, to: '/admin/integrations/apis', order: 30 },
      },
      {
        id: 'admin-integrations-emby-legacy',
        path: 'integrations/emby',
        element: <Navigate to="/admin/emby/interfaces" replace />,
      },
      {
        id: 'admin-emby',
        path: 'emby',
        element: <Navigate to="/admin/emby/interfaces" replace />,
      },
      {
        id: 'admin-emby-interfaces',
        path: 'emby/interfaces',
        element: <AdminEmbyAPIsPage />,
        navigation: { scope: 'viewer', label: '播放器接口', icon: Code2, to: '/admin/emby/interfaces', order: 50, end: true },
      },
      {
        id: 'admin-integrations-notifications',
        path: 'integrations/notifications',
        element: <NotifyChannelsPage />,
        navigation: { scope: 'management', group: 'integrations', label: '通知渠道', icon: Bell, to: '/admin/integrations/notifications', order: 40 },
      },
      {
        id: 'admin-integrations-assistant',
        path: 'integrations/assistant',
        element: <AssistantChatPage />,
        navigation: { scope: 'management', group: 'integrations', label: 'AI 会话', icon: Bot, to: '/admin/integrations/assistant', order: 50 },
      },
      {
        id: 'admin-settings',
        path: 'settings',
        element: <Navigate to="/admin/settings/general" replace />,
      },
      {
        id: 'admin-settings-general',
        path: 'settings/general',
        element: <SettingsPage groupKey="general" />,
        navigation: { scope: 'management', group: 'settings', label: '常规', icon: SlidersHorizontal, to: '/admin/settings/general', order: 10 },
      },
      {
        id: 'admin-settings-playback',
        path: 'settings/playback',
        element: <SettingsPage groupKey="playback" />,
        navigation: { scope: 'management', group: 'settings', label: '播放与探测', icon: FileText, to: '/admin/settings/playback', order: 20 },
      },
      {
        id: 'admin-settings-recognition',
        path: 'settings/recognition-words',
        element: <SettingsPage groupKey="recognition-words" />,
        navigation: { scope: 'management', group: 'settings', label: '识别词', icon: ListChecks, to: '/admin/settings/recognition-words', order: 30 },
      },
      {
        id: 'admin-settings-access',
        path: 'settings/access',
        element: <SettingsPage groupKey="access" />,
        navigation: { scope: 'management', group: 'settings', label: '内容访问', icon: KeyRound, to: '/admin/settings/access', order: 40 },
      },
    ],
  },

  { id: 'legacy-files', path: 'files', element: <Navigate to="/admin/media/files" replace />, adminOnly: true },
  { id: 'legacy-strm', path: 'strm', element: <Navigate to="/admin/media/strm" replace />, adminOnly: true },
  { id: 'legacy-storage', path: 'storage', element: <Navigate to="/admin/storage" replace />, adminOnly: true },
  { id: 'legacy-tools', path: 'tools', element: <Navigate to="/admin/storage" replace />, adminOnly: true },
  { id: 'legacy-duplicates', path: 'duplicates', element: <Navigate to="/admin/storage/duplicates" replace />, adminOnly: true },
  { id: 'legacy-recycle', path: 'recycle', element: <Navigate to="/admin/storage/recycle" replace />, adminOnly: true },
  { id: 'legacy-tasks', path: 'tasks', element: <Navigate to="/admin/tasks" replace />, adminOnly: true },
  { id: 'legacy-scheduler', path: 'scheduler', element: <Navigate to="/admin/tasks?panel=scheduler" replace />, adminOnly: true },
  { id: 'legacy-stats', path: 'stats', element: <Navigate to="/admin/tasks/stats" replace />, adminOnly: true },
  { id: 'legacy-notify', path: 'notify-channels', element: <Navigate to="/admin/integrations/notifications" replace />, adminOnly: true },
  { id: 'legacy-assistant', path: 'assistant', element: <Navigate to="/admin/integrations/assistant" replace />, adminOnly: true },
  { id: 'legacy-api-configs', path: 'api-configs', element: <Navigate to="/admin/integrations/apis" replace />, adminOnly: true },
  { id: 'legacy-settings', path: 'settings', element: <Navigate to="/admin/settings/general" replace />, adminOnly: true },
]
