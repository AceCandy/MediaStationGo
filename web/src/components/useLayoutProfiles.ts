import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import toast from 'react-hot-toast'

import { playProfilesAPI } from '../api/play_profiles'
import type { PlayProfile, User } from '../types'
import { requestPIN } from './requestPIN'

type UseLayoutProfilesOptions = {
  activeProfileId: string | null
  setActiveProfile: (id: string | null, pinToken?: string | null) => void
  user: User | null | undefined
}

export function useLayoutProfiles({
  activeProfileId,
  setActiveProfile,
  user,
}: UseLayoutProfilesOptions) {
  const [isProfileOpen, setIsProfileOpen] = useState(false)
  const [profiles, setProfiles] = useState<PlayProfile[]>([])
  const loadedUserID = useRef<string | null>(null)

  useEffect(() => {
    if (!user) {
      loadedUserID.current = null
      setProfiles([])
      setActiveProfile(null)
      return
    }
    const activeProfileKnown = !activeProfileId || profiles.length === 0 || profiles.some((profile) => profile.id === activeProfileId)
    if (loadedUserID.current === user.id && activeProfileKnown) return
    let cancelled = false
    playProfilesAPI
      .list()
      .then((rows) => {
        if (cancelled) return
        loadedUserID.current = user.id
        setProfiles(rows)
        const active = rows.find((profile) => profile.id === activeProfileId)
        if (!active) {
          const defaultProfile = rows.find((profile) => profile.is_default && !profile.require_pin)
          setActiveProfile(defaultProfile?.id ?? null)
        }
      })
      .catch(() => undefined)
    return () => { cancelled = true }
  }, [activeProfileId, profiles, setActiveProfile, user])

  const activeProfile = useMemo(
    () => profiles.find((profile) => profile.id === activeProfileId) ?? null,
    [activeProfileId, profiles],
  )

  const switchProfile = useCallback(
    async (profile: PlayProfile) => {
      if (activeProfileId === profile.id) {
        setIsProfileOpen(false)
        return
      }
      try {
        let pinToken: string | null = null
        if (profile.require_pin) {
          const pin = await requestPIN({ profileName: profile.name })
          if (!pin) return
          const verified = await playProfilesAPI.verifyPin(profile.id, pin)
          pinToken = verified.token
        }
        setActiveProfile(profile.id, pinToken)
        setIsProfileOpen(false)
        toast.success(`已切换到「${profile.name}」`)
      } catch (err: unknown) {
        const msg =
          (err as { response?: { data?: { error?: string } } })?.response?.data?.error ??
          'PIN 验证失败'
        toast.error(msg)
      }
    },
    [activeProfileId, setActiveProfile],
  )

  const useDefaultProfile = useCallback(() => {
    setActiveProfile(null)
    setIsProfileOpen(false)
  }, [setActiveProfile])

  return {
    activeProfile,
    isProfileOpen,
    profiles,
    setIsProfileOpen,
    switchProfile,
    useDefaultProfile,
  }
}
