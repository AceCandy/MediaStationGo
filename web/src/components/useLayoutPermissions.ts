import { useCallback, useEffect } from 'react'

import { usePermissionStore } from '../stores/permissions'
import type { User } from '../types'

export function useLayoutPermissions(user: User | null | undefined) {
  const permissions = usePermissionStore((state) => state.permissions)
  const isSuper = usePermissionStore((state) => state.isSuper)
  const isPermissionLoading = usePermissionStore((state) => state.isLoading)
  const error = usePermissionStore((state) => state.error)
  const permissionUserId = usePermissionStore((state) => state.userId)
  const loadedUserId = usePermissionStore((state) => state.loadedUserId)
  const fetchPermissions = usePermissionStore((state) => state.fetchPermissions)
  const clearPermissions = usePermissionStore((state) => state.clearPermissions)
  const hasSuperAccess = user?.role === 'admin' || user?.tier === 'plus'

  useEffect(() => {
    if (!user) return
    if (hasSuperAccess) {
      if (permissionUserId) clearPermissions()
      return
    }
    if (permissionUserId !== user.id) {
      clearPermissions()
      void fetchPermissions(user.id)
    }
  }, [clearPermissions, fetchPermissions, hasSuperAccess, permissionUserId, user])

  const isAdmin = user?.role === 'admin'
  const isReady = Boolean(hasSuperAccess || (user && loadedUserId === user.id))
  const can = useCallback(
    (key: string) =>
      Boolean(
        hasSuperAccess ||
          (user && loadedUserId === user.id && (isSuper || (permissions ?? {})[key] === true)),
      ),
    [hasSuperAccess, isSuper, loadedUserId, permissions, user],
  )
  const retry = useCallback(() => {
    if (user && !hasSuperAccess) void fetchPermissions(user.id)
  }, [fetchPermissions, hasSuperAccess, user])

  return {
    can,
    error: permissionUserId === user?.id ? error : null,
    isAdmin,
    isLoading: !isReady && (isPermissionLoading || permissionUserId !== user?.id),
    isReady,
    retry,
  }
}
