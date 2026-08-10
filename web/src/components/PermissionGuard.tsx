import { type ReactNode } from 'react'

import { useAuthStore } from '../stores/auth'
import { useLayoutPermissions } from './useLayoutPermissions'

interface PermissionGuardProps {
  permission: string
  children: ReactNode
  fallback?: ReactNode
  requireSuperUser?: boolean
}

/**
 * PermissionGuard 组件用于根据用户权限控制内容显示。
 * 
 * @param permission - 需要的权限键
 * @param children - 有权限时显示的内容
 * @param fallback - 无权限时显示的内容（可选，默认不显示）
 * @param requireSuperUser - 是否要求超级用户（admin/plus）绕过权限检查
 */
export function PermissionGuard({ 
  permission, 
  children, 
  fallback = null,
  requireSuperUser = false,
}: PermissionGuardProps) {
  const user = useAuthStore((state) => state.user)
  const permissions = useLayoutPermissions(user)

  // 超级用户（admin 或 plus）默认有所有权限
  if (user?.tier === 'plus' || user?.role === 'admin') {
    return <>{children}</>
  }

  // 如果 requireSuperUser 为 true 且用户不是超级用户，则不显示
  if (requireSuperUser) {
    return <>{fallback}</>
  }

  // 加载中时显示 fallback
  if (!permissions.isReady) {
    return <>{fallback}</>
  }

  // 检查具体权限
  if (permissions.can(permission)) {
    return <>{children}</>
  }

  return <>{fallback}</>
}
