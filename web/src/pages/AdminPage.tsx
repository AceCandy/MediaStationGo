import type { ReactNode } from 'react'
import { LibraryBig } from 'lucide-react'

import { APIConfigsPanel } from '../components/APIConfigsPanel'
import { AdminLibraryPanel } from './AdminLibraryPanel'
import { AdminUsersPanel } from './AdminUsersPanel'

export function AdminMediaPage() {
  return (
    <div className="space-y-6">
      <PageHeading
        icon={<LibraryBig size={20} />}
        title="媒体库管理"
        description="创建媒体库并维护入库路径。"
      />
      <AdminLibraryPanel />
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

function PageHeading({
  title,
  description,
  icon,
}: {
  title: string
  description: string
  icon?: ReactNode
}) {
  return (
    <header className="flex items-center gap-4">
      {icon ? <div className="modal-icon">{icon}</div> : null}
      <div>
        <h1 className="font-display text-3xl font-bold text-[var(--app-text)]">{title}</h1>
        <p className="mt-1 text-sm text-[var(--app-muted)]">{description}</p>
      </div>
    </header>
  )
}
