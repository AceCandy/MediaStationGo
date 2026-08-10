import { APIConfigsPanel } from '../components/APIConfigsPanel'
import { ManagementShortcuts } from '../components/ManagementShortcuts'
import { AdminLibraryPanel } from './AdminLibraryPanel'
import { AdminUsersPanel } from './AdminUsersPanel'

export function AdminPage() {
  return (
    <div className="space-y-6">
      <PageHeading title="管理总览" description="按工作域进入现有管理能力。" />
      <ManagementShortcuts
        title="管理工作域"
        items={[
          { to: '/admin/media', title: '媒体入库', description: '媒体库、文件整理与 STRM 工具' },
          { to: '/admin/storage', title: '存储与清理', description: '存储概览、重复文件与回收站' },
          { to: '/admin/tasks', title: '任务状态', description: '任务列表、调度器与运行统计' },
          { to: '/admin/integrations', title: '用户与集成', description: '用户、外部 API、通知与 AI 会话' },
          { to: '/admin/settings', title: '系统设置', description: '系统参数、识别词与更新设置' },
        ]}
      />
    </div>
  )
}

export function AdminMediaPage() {
  return (
    <div className="space-y-8">
      <PageHeading title="媒体入库" description="管理媒体库，并进入文件整理或 STRM 工作流。" />
      <ManagementShortcuts
        title="入库工具"
        items={[
          { to: '/admin/media/files', title: '文件与入库', description: '浏览文件并手动整理入库' },
          { to: '/admin/media/strm', title: 'STRM 工具', description: '生成、修复、导入与绑定 STRM' },
        ]}
      />
      <AdminLibraryPanel />
    </div>
  )
}

export function AdminIntegrationsPage() {
  return (
    <div className="space-y-6">
      <PageHeading title="用户与集成" description="管理访问用户和外部服务连接。" />
      <ManagementShortcuts
        title="集成能力"
        items={[
          { to: '/admin/integrations/users', title: '用户管理', description: '账户、状态、密码与权限' },
          { to: '/admin/integrations/apis', title: '外部 API', description: 'TMDb、Douban、Bangumi 与 AI 配置' },
          { to: '/admin/integrations/notifications', title: '通知渠道', description: 'Bot、Webhook 与邮件通知' },
          { to: '/admin/integrations/assistant', title: 'AI 会话', description: '后台多轮会话与操作记录' },
        ]}
      />
    </div>
  )
}

export function AdminUsersPage() {
  return (
    <div className="space-y-6">
      <PageHeading title="用户管理" description="管理账户状态、密码与访问权限。" />
      <AdminUsersPanel />
    </div>
  )
}

export function AdminAPIsPage() {
  return (
    <div className="space-y-6">
      <PageHeading title="外部 API" description="配置发现、元数据和 AI 服务提供方。" />
      <APIConfigsPanel />
    </div>
  )
}

function PageHeading({ title, description }: { title: string; description: string }) {
  return (
    <div>
      <h1 className="font-display text-3xl font-bold text-[var(--app-text)]">{title}</h1>
      <p className="mt-1 text-sm text-[var(--app-muted)]">{description}</p>
    </div>
  )
}
