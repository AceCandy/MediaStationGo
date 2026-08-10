import { ReactNode } from 'react'
import { Link, Navigate, useLocation } from 'react-router-dom'

import { useAuthStore } from '../stores/auth'
import { useLayoutPermissions } from './useLayoutPermissions'

// Route guard: redirects to /login when no token is present.
export function RequireAuth({ children }: { children: ReactNode }) {
  const token = useAuthStore((s) => s.token)
  const location = useLocation()
  if (!token) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }
  return <>{children}</>
}

// Route guard: only allows users with role === "admin".
export function RequireAdmin({ children }: { children: ReactNode }) {
  const user = useAuthStore((s) => s.user)
  if (user?.role !== 'admin') {
    return <Navigate to="/" replace />
  }
  return <>{children}</>
}

export function RequirePermission({
  children,
  permission,
  deniedTo = '/',
}: {
  children: ReactNode
  permission: string
  deniedTo?: string
}) {
  const user = useAuthStore((state) => state.user)
  const permissions = useLayoutPermissions(user)

  if (!permissions.isReady) {
    if (permissions.error) {
      return (
        <div className="mx-auto max-w-lg py-12 text-center">
          <p className="font-display text-xl font-bold text-[var(--app-text)]">权限信息加载失败</p>
          <p className="mt-2 text-sm text-[var(--app-muted)]">请重试，或返回首页继续浏览。</p>
          <div className="mt-5 flex justify-center gap-3">
            <button type="button" onClick={permissions.retry} className="neon-button">
              重试
            </button>
            <Link to="/" replace className="rounded-xl border border-[var(--app-border)] px-4 py-2 text-sm font-bold">
              返回首页
            </Link>
          </div>
        </div>
      )
    }
    return <p className="px-6 py-8 text-[var(--app-muted)]">正在确认访问权限…</p>
  }

  if (!permissions.can(permission)) return <Navigate to={deniedTo} replace />
  return <>{children}</>
}
