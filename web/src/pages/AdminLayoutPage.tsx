import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { ArrowLeft, Settings } from 'lucide-react'
import clsx from 'clsx'

import { resolveAppRoutes, type ManagementGroupID } from '../appRoutes'

const MANAGEMENT_GROUPS: Array<{ id: ManagementGroupID; label: string }> = [
  { id: 'media', label: '媒体入库' },
  { id: 'storage', label: '存储与清理' },
  { id: 'tasks', label: '任务状态' },
  { id: 'integrations', label: '用户与集成' },
  { id: 'settings', label: '系统设置' },
]

const MANAGEMENT_ITEMS = resolveAppRoutes()
  .filter(({ route }) => route.navigation?.scope === 'management')
  .map(({ route }) => route.navigation!)

export function AdminLayoutPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const selectedRoute = [...MANAGEMENT_ITEMS]
    .sort((left, right) => right.to.length - left.to.length)
    .find((item) => location.pathname === item.to || location.pathname.startsWith(`${item.to}/`))

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-4 border-b border-[var(--app-border)] pb-5">
        <div className="flex items-center gap-3">
          <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-[var(--app-active-bg)] text-[var(--app-active-icon)]">
            <Settings size={20} />
          </span>
          <div>
            <p className="font-display text-2xl font-bold text-[var(--app-text)]">管理空间</p>
            <p className="text-sm text-[var(--app-muted)]">媒体、存储、任务、集成与系统设置</p>
          </div>
        </div>
        <NavLink
          to="/"
          className="flex min-h-11 items-center gap-2 rounded-lg border border-[var(--app-border)] px-4 text-sm font-bold text-[var(--app-subtle)] hover:bg-[var(--app-hover)]"
        >
          <ArrowLeft size={16} />
          返回观看空间
        </NavLink>
      </header>

      <label className="block xl:hidden">
        <span className="sr-only">管理页面</span>
        <select
          value={selectedRoute?.to ?? '/admin'}
          onChange={(event) => navigate(event.target.value)}
          className="input-base min-h-11"
          aria-label="选择管理页面"
        >
          <option value="/admin">管理总览</option>
          {MANAGEMENT_GROUPS.map((group) => (
            <optgroup key={group.id} label={group.label}>
              {MANAGEMENT_ITEMS.filter((item) => item.group === group.id).map((item) => (
                <option key={item.to} value={item.to}>{item.label}</option>
              ))}
            </optgroup>
          ))}
        </select>
      </label>

      <div className="grid min-w-0 gap-8 xl:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="hidden xl:block">
          <nav aria-label="管理导航" className="sticky top-0 space-y-5">
            <AdminNavLink to="/admin" label="管理总览" end />
            {MANAGEMENT_GROUPS.map((group) => (
              <div key={group.id} className="space-y-1">
                <p className="px-3 pb-1 text-xs font-bold text-[var(--app-muted)]">{group.label}</p>
                {MANAGEMENT_ITEMS.filter((item) => item.group === group.id).map((item) => (
                  <AdminNavLink key={item.to} to={item.to} label={item.label} />
                ))}
              </div>
            ))}
          </nav>
        </aside>
        <div className="min-w-0 overflow-x-auto">
          <Outlet />
        </div>
      </div>
    </div>
  )
}

function AdminNavLink({ to, label, end = false }: { to: string; label: string; end?: boolean }) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) => clsx(
        'flex min-h-11 items-center rounded-lg px-3 text-sm font-semibold transition-colors',
        isActive
          ? 'bg-[var(--app-active-bg)] text-[var(--app-active-text)]'
          : 'text-[var(--app-subtle)] hover:bg-[var(--app-hover)] hover:text-[var(--app-text)]',
      )}
    >
      {label}
    </NavLink>
  )
}
