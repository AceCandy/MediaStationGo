import { useAuthStore } from '../stores/auth'
import { usePlayProfileStore } from '../stores/playProfile'

// 身份与档案切换时重建来源页面，旧请求和收藏状态不能被新身份复用。
export function useMediaAccessKey() {
  const userID = useAuthStore((state) => state.user?.id ?? '')
  const profileID = usePlayProfileStore((state) => state.activeProfileId ?? '')
  const unlocked = usePlayProfileStore((state) => Boolean(state.activeProfilePinToken))
  return `${userID}:${profileID}:${unlocked}`
}
