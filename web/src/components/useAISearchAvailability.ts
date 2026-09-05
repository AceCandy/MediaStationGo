import { useEffect, useState } from 'react'

import { aiAPI } from '../api/ai'
import { useAuthStore } from '../stores/auth'
import { usePlayProfileStore } from '../stores/playProfile'
import { useLayoutPermissions } from './useLayoutPermissions'

export function useAISearchAvailability() {
  const user = useAuthStore((state) => state.user)
  const profileId = usePlayProfileStore((state) => state.activeProfileId)
  const { can, isReady } = useLayoutPermissions(user)
  const allowed = isReady && can('can_use_ai')
  const key = `${user?.id ?? ''}:${profileId ?? ''}`
  const [status, setStatus] = useState<{ key: string; enabled: boolean } | null>(null)

  useEffect(() => {
    if (!allowed) return
    let active = true
    setStatus(null)
    aiAPI.status()
      .then((result) => { if (active) setStatus({ key, enabled: result.enabled }) })
      .catch(() => { if (active) setStatus({ key, enabled: false }) })
    return () => { active = false }
  }, [allowed, key])

  return {
    aiAvailable: allowed && status?.key === key && status.enabled,
    aiChecked: isReady && (!allowed || status?.key === key),
  }
}
